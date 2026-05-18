package scoring

import (
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"sync"
)

// KNN armazena os vetores pré-decodificados como float32 no heap do Go.
// float32 usa 168 MB para 3M×14 features (vs 336 MB em float64), mantendo
// o budget de RAM por instância dentro do limite do Docker.
type KNN struct {
	vectors []float32 // flat row-major: [N × 14], i-ésimo vetor em vectors[i*14:]
	frauds  []byte    // [N]: 1=fraud, 0=legit
	n, k    int
}

// knnEntry é o elemento do buffer top-K dentro de Score.
// Definida no escopo do pacote para ser acessível por indexOfMax.
type knnEntry struct {
	dSq   float32
	fraud bool
}

// NewKNN decodifica o binário flat em arrays Go na inicialização — O(N), rápido.
// Pré-decodificar aqui elimina 42M chamadas binary.LittleEndian por requisição.
//
// Formato esperado (little-endian):
//
//	[8 bytes]  uint64  — N registros
//	[N × 57 bytes]     — [14]float32 + 1 byte fraud flag
func NewKNN(data []byte, k int) (*KNN, error) {
	const recSize = 14*4 + 1 // 57 bytes por registro
	if len(data) < 8 {
		return nil, fmt.Errorf("dados insuficientes")
	}
	n := int(binary.LittleEndian.Uint64(data[:8]))
	if len(data) < 8+n*recSize {
		return nil, fmt.Errorf("dados truncados: esperado %d bytes, recebido %d", 8+n*recSize, len(data))
	}

	vectors := make([]float32, n*14)
	frauds := make([]byte, n)

	body := data[8:]
	for i := range n {
		rec := body[i*recSize:]
		for j := range 14 {
			vectors[i*14+j] = math.Float32frombits(binary.LittleEndian.Uint32(rec[j*4:]))
		}
		frauds[i] = rec[56]
	}

	return &KNN{vectors: vectors, frauds: frauds, n: n, k: k}, nil
}

// Score retorna a proporção de vizinhos com label "fraud" entre os K mais próximos.
// Complexidade: O(N·d + N·K) — brute-force com buffer top-K de tamanho fixo.
//
// Otimizações em relação à versão inicial:
//   - Top-K buffer fixo (k=5) em vez de sort.Slice sobre 3M pares → O(N·K) vs O(N·log N)
//   - Distâncias quadradas: elimina 3M chamadas math.Sqrt (sqrt é monótona, não altera ordem)
//   - float32 no inner loop: mais cache-friendly, sem decodificação de bytes por query
//   - Scan particionado em GOMAXPROCS goroutines: com GOMAXPROCS > 1 (definido via env no
//     docker-compose), cada goroutine processa N/P vetores em um OS thread distinto,
//     reduzindo a latência de wall-clock de X ms para ~X/P ms por requisição.
func (knn *KNN) Score(v [14]float64) float64 {
	var q [14]float32
	for i, f := range v {
		q[i] = float32(f)
	}

	// nChunks = GOMAXPROCS: cada chunk roda em um OS thread separado.
	// Com GOMAXPROCS=1 (padrão cgroup), cai de volta para 1 goroutine sem overhead relevante.
	nChunks := runtime.GOMAXPROCS(0)
	chunkSize := (knn.n + nChunks - 1) / nChunks

	partials := make([][]knnEntry, nChunks)
	var wg sync.WaitGroup
	wg.Add(nChunks)

	vecs := knn.vectors
	frds := knn.frauds

	for c := range nChunks {
		lo := c * chunkSize
		hi := min(lo+chunkSize, knn.n)
		go func(c, lo, hi int) {
			defer wg.Done()
			buf := make([]knnEntry, 0, knn.k)
			maxDSq := float32(math.MaxFloat32)
			maxIdx := 0

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
			partials[c] = buf
		}(c, lo, hi)
	}

	wg.Wait()

	// Merge: no máximo nChunks×K candidatos (ex: 4×5=20) → extrai top-K global.
	// O(nChunks × K²) — desprezível frente ao scan de 3M vetores.
	topK := make([]knnEntry, 0, knn.k)
	maxDSq := float32(math.MaxFloat32)
	maxIdx := 0
	for _, p := range partials {
		for _, e := range p {
			if len(topK) < knn.k {
				topK = append(topK, e)
				if len(topK) == knn.k {
					maxIdx = indexOfMax(topK)
					maxDSq = topK[maxIdx].dSq
				}
			} else if e.dSq < maxDSq {
				topK[maxIdx] = e
				maxIdx = indexOfMax(topK)
				maxDSq = topK[maxIdx].dSq
			}
		}
	}

	if len(topK) == 0 {
		return 0
	}
	fraudCount := 0
	for _, e := range topK {
		if e.fraud {
			fraudCount++
		}
	}
	return float64(fraudCount) / float64(len(topK))
}

// indexOfMax retorna o índice do elemento com maior dSq no buffer top-K.
// O(K) — K=5, custo desprezível frente ao loop principal de 3M iterações.
func indexOfMax(buf []knnEntry) int {
	idx := 0
	for i := 1; i < len(buf); i++ {
		if buf[i].dSq > buf[idx].dSq {
			idx = i
		}
	}
	return idx
}
