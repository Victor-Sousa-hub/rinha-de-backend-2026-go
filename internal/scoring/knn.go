package scoring

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"math"
	"sort"
)

type reference struct {
	Vector [14]float64 `json:"vector"`
	Label  string      `json:"label"`
}

type KNN struct {
	refs []reference
	k    int
}

// NewKNN carrega o dataset de referência uma única vez na inicialização.
// Descomprimir + decodificar JSON acontece aqui; nas requisições só há leitura
// do slice em memória — sem I/O, sem lock, seguro para acesso concorrente.
func NewKNN(gzData []byte, k int) (*KNN, error) {
	r, err := gzip.NewReader(bytes.NewReader(gzData))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var refs []reference
	if err := json.NewDecoder(r).Decode(&refs); err != nil {
		return nil, err
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
		pairs[i] = pair{d: euclidean(v, ref.Vector), fraud: ref.Label == "fraud"}
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
