package scoring

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

type reference struct {
	Vector [14]float64
	fraud  bool
}

type KNN struct {
	refs []reference
	k    int
}

// NewKNN carrega o dataset a partir do formato binário flat gerado por cmd/convert.
//
// Formato esperado (little-endian):
//
//	[8 bytes]  uint64  — número de registros N
//	[N × 113 bytes]    — registros
//	  [112 bytes]  [14]float64
//	  [1 byte]     fraud: 1=fraud, 0=legit
//
// Leitura de bytes é ~50× mais rápida que gzip + json.Decode para 3M registros:
// não há parser, não há alocações de string — só uma passagem linear pelo slice.
func NewKNN(data []byte, k int) (*KNN, error) {
	const recSize = 14*8 + 1 // 113 bytes por registro

	if len(data) < 8 {
		return nil, fmt.Errorf("dados insuficientes")
	}

	n := int(binary.LittleEndian.Uint64(data[:8]))
	body := data[8:]

	if len(body) < n*recSize {
		return nil, fmt.Errorf("dados truncados: esperado %d bytes, recebido %d", n*recSize, len(body))
	}

	refs := make([]reference, n)
	for i := range refs {
		rec := body[i*recSize:]
		for j := range refs[i].Vector {
			refs[i].Vector[j] = math.Float64frombits(binary.LittleEndian.Uint64(rec[j*8:]))
		}
		refs[i].fraud = rec[112] == 1
	}

	return &KNN{refs: refs, k: k}, nil
}

// euclidean distance entre dois vetores de 14 dimensões.
// Dimensões com sentinela -1: distância 0 se ambos são -1, 1 se apenas um é -1.
func euclidean(a, b [14]float64) float64 {
	var sum float64
	for i := range a {
		av, bv := a[i], b[i]
		if av < 0 || bv < 0 {
			if av != bv {
				sum += 1.0
			}
			continue
		}
		d := av - bv
		sum += d * d
	}
	return math.Sqrt(sum)
}

// Score retorna a proporção de vizinhos com label "fraud" entre os K mais próximos.
// Complexidade: O(n·d + n·log n), onde n = ~3M refs e d = 14 dimensões.
// Brute-force é viável aqui porque o dataset cabe inteiro em RAM e não há
// escrita concorrente — uma estrutura de índice (KD-tree, HNSW) seria mais
// rápida, mas adiciona complexidade desnecessária no momento.
func (knn *KNN) Score(v [14]float64) float64 {
	type pair struct {
		d     float64
		fraud bool
	}

	// Calcula distância de v para todos os pontos do dataset.
	pairs := make([]pair, len(knn.refs))
	for i, ref := range knn.refs {
		pairs[i] = pair{d: euclidean(v, ref.Vector), fraud: ref.fraud}
	}

	// Ordena por distância crescente; os K primeiros são os vizinhos mais próximos.
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].d < pairs[j].d
	})

	k := min(knn.k, len(pairs))

	fraudCount := 0
	for _, p := range pairs[:k] {
		if p.fraud {
			fraudCount++
		}
	}
	return float64(fraudCount) / float64(k)
}
