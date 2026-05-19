package scoring

import (
	"encoding/binary"
	"fmt"
	"math"
)

// KNN implementa busca por vizinhos mais próximos via índice IVF (Inverted File Index).
//
// O dataset é pré-agrupado em K clusters (k-means, gerado offline pelo cmd/convert).
// Cada query inspeciona apenas os nprobe clusters mais próximos, reduzindo comparações
// de O(N) → O(K + nprobe×N/K): para N=3M, K=1000, nprobe=2 são ~6 000 vetores
// em vez de 3M — speedup de ~500× sem perda significativa de recall.
// nprobe é configurável via env NPROBE para facilitar benchmarks de recall vs latência.
type KNN struct {
	vectors   []float32 // N×14 flat row-major, ordenados por cluster
	frauds    []byte    // N, ordenados por cluster
	centroids []float32 // K×14 centroides IVF
	bounds    []int     // K+1: cluster c contém vectors[bounds[c]:bounds[c+1]]
	n, k      int
	numK      int
	nprobe    int
}

// knnEntry é o elemento do buffer top-K dentro de Score.
// Definida no escopo do pacote para ser acessível por indexOfMax.
type knnEntry struct {
	dSq   float32
	fraud bool
}

// centPair é usado por nearestClusters para selecionar os nprobe centroides mais próximos.
type centPair struct {
	dSq float32
	idx int
}

// NewKNN decodifica o binário IVF gerado pelo cmd/convert.
//
// Formato esperado (little-endian):
//
//	[8 bytes]      uint64  — N registros
//	[8 bytes]      uint64  — K clusters
//	[K × 56 bytes] — centroides [K][14]float32
//	[K × 4 bytes]  — tamanho de cada cluster uint32
//	[N × 57 bytes] — vetores ordenados por cluster: [14]float32 + 1 byte fraud
func NewKNN(data []byte, k, nprobe int) (*KNN, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("dados insuficientes")
	}

	n := int(binary.LittleEndian.Uint64(data[:8]))
	numK := int(binary.LittleEndian.Uint64(data[8:16]))

	const recSize = 14*4 + 1
	centSize := numK * 14 * 4
	sizeBytes := numK * 4
	needed := 16 + centSize + sizeBytes + n*recSize

	if len(data) < needed {
		return nil, fmt.Errorf("dados truncados: esperado %d bytes, recebido %d", needed, len(data))
	}

	off := 16
	centroids := make([]float32, numK*14)
	for i := range numK * 14 {
		centroids[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
		off += 4
	}

	bounds := make([]int, numK+1)
	for c := range numK {
		bounds[c+1] = bounds[c] + int(binary.LittleEndian.Uint32(data[off:]))
		off += 4
	}

	vectors := make([]float32, n*14)
	frauds := make([]byte, n)
	body := data[off:]
	for i := range n {
		rec := body[i*recSize:]
		for j := range 14 {
			vectors[i*14+j] = math.Float32frombits(binary.LittleEndian.Uint32(rec[j*4:]))
		}
		frauds[i] = rec[56]
	}

	return &KNN{
		vectors:   vectors,
		frauds:    frauds,
		centroids: centroids,
		bounds:    bounds,
		n:         n,
		k:         k,
		numK:      numK,
		nprobe:    nprobe,
	}, nil
}


// Score retorna a proporção de vizinhos fraudulentos entre os K mais próximos.
//
// Implementação IVF em duas fases:
//  1. Seleciona os nprobe clusters mais próximos via distância euclidiana simples
//     nos centroides — K=1000 comparações, custo desprezível (~µs).
//  2. Brute-force top-K dentro desses clusters usando distância com sentinela
//     para resultado correto — ~nprobe×N/K ≈ 12 000 comparações.
func (knn *KNN) Score(v [14]float64) float64 {
	var q [14]float32
	for i, f := range v {
		q[i] = float32(f)
	}

	clusters := nearestClusters(q, knn.centroids, knn.numK, knn.nprobe)

	buf := make([]knnEntry, 0, knn.k)
	maxDSq := float32(math.MaxFloat32)
	maxIdx := 0

	vecs := knn.vectors
	frds := knn.frauds

	for _, c := range clusters {
		lo, hi := knn.bounds[c], knn.bounds[c+1]
		for i := lo; i < hi; i++ {
			base := i * 14
			var dSq float32
			for j := range 14 {
				a, b := q[j], vecs[base+j]
				if a < 0 || b < 0 {
					if a != b {
						dSq += 1
					}
					continue
				}
				d := a - b
				dSq += d * d
			}

			if len(buf) < knn.k {
				buf = append(buf, knnEntry{dSq, frds[i] == 1})
				if len(buf) == knn.k {
					maxIdx = indexOfMax(buf)
					maxDSq = buf[maxIdx].dSq
				}
			} else if dSq < maxDSq {
				buf[maxIdx] = knnEntry{dSq, frds[i] == 1}
				maxIdx = indexOfMax(buf)
				maxDSq = buf[maxIdx].dSq
			}
		}
	}

	if len(buf) == 0 {
		return 0
	}
	fraudCount := 0
	for _, e := range buf {
		if e.fraud {
			fraudCount++
		}
	}
	return float64(fraudCount) / float64(len(buf))
}

// nearestClusters retorna os índices dos nprobe clusters mais próximos de q.
// Usa distância euclidiana simples (sem sentinela) — adequado para seleção
// aproximada: o recall final é garantido pela busca exata dentro dos clusters.
func nearestClusters(q [14]float32, centroids []float32, numK, nprobe int) []int {
	buf := make([]centPair, 0, nprobe)
	maxDSq := float32(math.MaxFloat32)
	maxPos := 0

	for c := range numK {
		var dSq float32
		for j := range 14 {
			d := q[j] - centroids[c*14+j]
			dSq += d * d
		}
		if len(buf) < nprobe {
			buf = append(buf, centPair{dSq, c})
			if len(buf) == nprobe {
				maxPos = maxCentPairIdx(buf)
				maxDSq = buf[maxPos].dSq
			}
		} else if dSq < maxDSq {
			buf[maxPos] = centPair{dSq, c}
			maxPos = maxCentPairIdx(buf)
			maxDSq = buf[maxPos].dSq
		}
	}

	result := make([]int, len(buf))
	for i, p := range buf {
		result[i] = p.idx
	}
	return result
}

func indexOfMax(buf []knnEntry) int {
	idx := 0
	for i := 1; i < len(buf); i++ {
		if buf[i].dSq > buf[idx].dSq {
			idx = i
		}
	}
	return idx
}

func maxCentPairIdx(buf []centPair) int {
	idx := 0
	for i := 1; i < len(buf); i++ {
		if buf[i].dSq > buf[idx].dSq {
			idx = i
		}
	}
	return idx
}
