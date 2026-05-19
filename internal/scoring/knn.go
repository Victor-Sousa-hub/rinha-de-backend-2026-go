package scoring

import (
	"encoding/binary"
	"fmt"
	"math"
)

// maxK e maxNprobe são os tetos dos arrays alocados no stack dentro de Score.
// Arrays de tamanho fixo e pequeno ficam no stack — o GC nunca os vê.
// Aumentar esses valores não tem custo em tempo ou memória fora de Score.
const (
	maxK      = 5
	maxNprobe = 8
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

type knnEntry struct {
	dSq   float32
	fraud bool
}

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
	if k > maxK {
		return nil, fmt.Errorf("k=%d excede maxK=%d; ajuste a constante em knn.go", k, maxK)
	}
	if nprobe > maxNprobe {
		return nil, fmt.Errorf("nprobe=%d excede maxNprobe=%d; ajuste a constante em knn.go", nprobe, maxNprobe)
	}
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
// Implementação IVF em duas fases, sem alocações de heap no hot path:
//  1. Seleciona os nprobe clusters mais próximos via distância euclidiana nos
//     centroides — array [maxNprobe]centPair no stack, zero GC pressure.
//  2. Brute-force top-K dentro desses clusters com sentinela para ausência —
//     array [maxK]knnEntry no stack, idem.
func (knn *KNN) Score(v [14]float64) float64 {
	var q [14]float32
	for i, f := range v {
		q[i] = float32(f)
	}

	// Fase 1: nprobe clusters mais próximos — stack-allocated.
	var cents [maxNprobe]centPair
	cLen := 0
	cMaxDSq := float32(math.MaxFloat32)
	cMaxPos := 0
	np := knn.nprobe

	for c := range knn.numK {
		var dSq float32
		for j := range 14 {
			d := q[j] - knn.centroids[c*14+j]
			dSq += d * d
		}
		if cLen < np {
			cents[cLen] = centPair{dSq, c}
			cLen++
			if cLen == np {
				cMaxPos = maxCentPairIdx(cents[:cLen])
				cMaxDSq = cents[cMaxPos].dSq
			}
		} else if dSq < cMaxDSq {
			cents[cMaxPos] = centPair{dSq, c}
			cMaxPos = maxCentPairIdx(cents[:cLen])
			cMaxDSq = cents[cMaxPos].dSq
		}
	}

	// Fase 2: top-K dentro dos clusters selecionados — stack-allocated.
	var top [maxK]knnEntry
	tLen := 0
	tMaxDSq := float32(math.MaxFloat32)
	tMaxIdx := 0

	vecs := knn.vectors
	frds := knn.frauds

	for ci := range cLen {
		c := cents[ci].idx
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
			if tLen < knn.k {
				top[tLen] = knnEntry{dSq, frds[i] == 1}
				tLen++
				if tLen == knn.k {
					tMaxIdx = indexOfMax(top[:tLen])
					tMaxDSq = top[tMaxIdx].dSq
				}
			} else if dSq < tMaxDSq {
				top[tMaxIdx] = knnEntry{dSq, frds[i] == 1}
				tMaxIdx = indexOfMax(top[:tLen])
				tMaxDSq = top[tMaxIdx].dSq
			}
		}
	}

	if tLen == 0 {
		return 0
	}
	fraudCount := 0
	for i := range tLen {
		if top[i].fraud {
			fraudCount++
		}
	}
	return float64(fraudCount) / float64(tLen)
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
