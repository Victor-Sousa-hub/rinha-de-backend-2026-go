// Conversor references.json.gz → resources/references.bin (formato IVF)
//
// Formato de saída (flat binary, little-endian):
//
//	[8 bytes]      uint64  — N registros
//	[8 bytes]      uint64  — K clusters
//	[K × 56 bytes] — centroides [K][14]float32
//	[K × 4 bytes]  — tamanho de cada cluster uint32
//	[N × 15 bytes] — vetores ordenados por cluster: [14]uint8 + 1 byte fraud
//
// A estrutura IVF (Inverted File Index) agrupa os N vetores em K clusters via
// k-means offline. Em runtime, Score() busca apenas nos nprobe clusters mais
// próximos da query, reduzindo O(N) para O(K + nprobe×N/K) — ~250× mais rápido
// com K=1000, nprobe=4 e N=3M.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"
)

const (
	kClusters  = 1000    // número de clusters IVF
	kSampleMax = 100_000 // tamanho máximo do sample para k-means
	kMaxIter   = 25      // iterações máximas do algoritmo de Lloyd
)

type ref struct {
	Vector [14]float64 `json:"vector"`
	Label  string      `json:"label"`
}

func main() {
	start := time.Now()

	data, err := os.ReadFile("resources/references.json.gz")
	if err != nil {
		log.Fatalf("leitura do gzip: %v", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		log.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()

	var refs []ref
	if err := json.NewDecoder(gr).Decode(&refs); err != nil {
		log.Fatalf("json.Decode: %v", err)
	}

	n := len(refs)
	log.Printf("carregados %d registros", n)

	// Converte para float32 flat antes do k-means.
	vecs := make([]float32, n*14)
	frauds := make([]byte, n)
	for i, r := range refs {
		for j, f64 := range r.Vector {
			vecs[i*14+j] = float32(f64)
		}
		if r.Label == "fraud" {
			frauds[i] = 1
		}
	}
	refs = nil // libera memória do JSON (~GB) antes do k-means

	sampleN := min(kSampleMax, n)
	log.Printf("k-means: K=%d sample=%d maxIter=%d", kClusters, sampleN, kMaxIter)
	tKmeans := time.Now()
	centroids := buildCentroids(vecs, n, sampleN)
	log.Printf("k-means: %s", time.Since(tKmeans).Round(time.Millisecond))

	log.Printf("atribuindo %d vetores (paralelo)...", n)
	tAssign := time.Now()
	assignments := assignAll(vecs, n, centroids)
	log.Printf("atribuição: %s", time.Since(tAssign).Round(time.Millisecond))

	// Ordena os índices por cluster para escrever os vetores agrupados no binário.
	order := make([]int, n)
	for i := range n {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		return assignments[order[a]] < assignments[order[b]]
	})

	sizes := make([]uint32, kClusters)
	for _, a := range assignments {
		sizes[a]++
	}

	out, err := os.Create("resources/references.bin")
	if err != nil {
		log.Fatalf("criar references.bin: %v", err)
	}
	defer out.Close()

	w := bufio.NewWriterSize(out, 4<<20)

	var hdr [16]byte
	binary.LittleEndian.PutUint64(hdr[:8], uint64(n))
	binary.LittleEndian.PutUint64(hdr[8:], uint64(kClusters))
	w.Write(hdr[:])

	var cf [4]byte
	for _, f := range centroids {
		binary.LittleEndian.PutUint32(cf[:], math.Float32bits(f))
		w.Write(cf[:])
	}

	var sf [4]byte
	for _, s := range sizes {
		binary.LittleEndian.PutUint32(sf[:], s)
		w.Write(sf[:])
	}

	var rec [15]byte
	for _, idx := range order {
		base := idx * 14
		for j := range 14 {
			rec[j] = quantize(vecs[base+j])
		}
		rec[14] = frauds[idx]
		w.Write(rec[:])
	}

	if err := w.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

	info, _ := out.Stat()
	log.Printf("OK: %d registros, K=%d → resources/references.bin (%.0f MB) em %s",
		n, kClusters, float64(info.Size())/(1<<20), time.Since(start))
}

// quantize mapeia float32 [0,1] → uint8 [0,254]; sentinela -1 → 255.
// 255 reservado para features ausentes preserva a semântica do sentinela float sem
// custo extra de bit: na distância, par (255,255) contribui 0 e par (255,x) contribui 1.
func quantize(v float32) uint8 {
	if v < 0 {
		return 255
	}
	q := v * 254.0
	if q > 254 {
		q = 254
	}
	return uint8(q + 0.5)
}

// buildCentroids executa o algoritmo de Lloyd em um sample aleatório dos vetores.
// Usar sample em vez de todos os N vetores reduz a complexidade de O(N×K×iter)
// para O(sampleN×K×iter), mantendo qualidade adequada — amostras de 100K são
// representativas o suficiente para K=1000 em datasets de 3M pontos.
func buildCentroids(vecs []float32, n, sampleN int) []float32 {
	const d = 14
	rng := rand.New(rand.NewSource(42))

	// perm[0:sampleN] define o sample; perm[0:kClusters] inicializa os centroides.
	perm := rng.Perm(n)

	centroids := make([]float32, kClusters*d)
	for i := range kClusters {
		src := perm[i]
		copy(centroids[i*d:], vecs[src*d:src*d+d])
	}

	assignments := make([]int, sampleN)

	for iter := range kMaxIter {
		changed := 0
		for i := range sampleN {
			best := nearestCentroid(vecs[perm[i]*d:perm[i]*d+d], centroids)
			if best != assignments[i] {
				assignments[i] = best
				changed++
			}
		}
		log.Printf("  [lloyd %2d] changed=%d", iter+1, changed)
		if iter > 0 && changed == 0 {
			break
		}

		newC := make([]float32, kClusters*d)
		counts := make([]int, kClusters)
		for i := range sampleN {
			c := assignments[i]
			src := perm[i]
			counts[c]++
			for j := range d {
				newC[c*d+j] += vecs[src*d+j]
			}
		}
		// Cluster vazio: reinicializa com vetor aleatório do sample para evitar
		// centroide zerado que distorce todos os outros centroides na próxima iteração.
		for c := range kClusters {
			if counts[c] == 0 {
				src := perm[rng.Intn(sampleN)]
				copy(newC[c*d:], vecs[src*d:src*d+d])
				counts[c] = 1
			}
			inv := float32(1) / float32(counts[c])
			for j := range d {
				newC[c*d+j] *= inv
			}
		}
		centroids = newC
	}

	return centroids
}

// assignAll atribui cada um dos n vetores ao centroide mais próximo.
// Paraleliza em runtime.NumCPU() goroutines — o converter roda sem restrição
// de CPU (máquina do desenvolvedor ou CI), então usa todos os cores disponíveis.
func assignAll(vecs []float32, n int, centroids []float32) []int {
	assignments := make([]int, n)
	nChunks := runtime.NumCPU()
	chunkSize := (n + nChunks - 1) / nChunks

	var wg sync.WaitGroup
	wg.Add(nChunks)
	for c := range nChunks {
		lo := c * chunkSize
		hi := min(lo+chunkSize, n)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				assignments[i] = nearestCentroid(vecs[i*14:i*14+14], centroids)
			}
		}(lo, hi)
	}
	wg.Wait()
	return assignments
}

// nearestCentroid usa distância euclidiana simples (sem tratamento de sentinela).
// Vetores com features ausentes (-1 nos dims 5 e 6) naturalmente se agrupam
// juntos, o que é o comportamento desejado: queries com last_transaction=null
// serão roteadas para clusters com perfil similar.
func nearestCentroid(vec, centroids []float32) int {
	const d = 14
	k := len(centroids) / d
	best := 0
	bestDSq := float32(math.MaxFloat32)
	for c := range k {
		var dSq float32
		for j := range d {
			diff := vec[j] - centroids[c*d+j]
			dSq += diff * diff
		}
		if dSq < bestDSq {
			bestDSq = dSq
			best = c
		}
	}
	return best
}
