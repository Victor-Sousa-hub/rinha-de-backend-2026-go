package scoring

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// KNN guarda apenas uma referência ao slice de bytes do embed — sem alocar
// os 324 MB de refs no heap. O OS mantém esses bytes no segmento .rodata do
// binário; como api1 e api2 rodam a mesma imagem, as páginas são compartilhadas
// entre os dois processos pelo kernel (CoW, read-only), contando ~uma vez no budget de RAM.
type KNN struct {
	data []byte // binário flat: [8 bytes N] + [N × 113 bytes registros]
	n    int
	k    int
}

// NewKNN valida o cabeçalho e armazena o slice — O(1), sem parsing.
func NewKNN(data []byte, k int) (*KNN, error) {
	const recSize = 14*8 + 1
	if len(data) < 8 {
		return nil, fmt.Errorf("dados insuficientes")
	}
	n := int(binary.LittleEndian.Uint64(data[:8]))
	if len(data) < 8+n*recSize {
		return nil, fmt.Errorf("dados truncados: esperado %d bytes, recebido %d", 8+n*recSize, len(data))
	}
	return &KNN{data: data, n: n, k: k}, nil
}

// refAt lê o i-ésimo registro diretamente dos bytes — sem alocação de struct intermediária.
func (knn *KNN) refAt(i int) ([14]float64, bool) {
	const recSize = 113
	rec := knn.data[8+i*recSize:]
	var vec [14]float64
	for j := range vec {
		vec[j] = math.Float64frombits(binary.LittleEndian.Uint64(rec[j*8:]))
	}
	return vec, rec[112] == 1
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

	// pairs é alocado por chamada (~48 MB) e descartado após o return —
	// o GC coleta entre requisições. É o custo inevitável do brute-force.
	pairs := make([]pair, knn.n)
	for i := range pairs {
		vec, fraud := knn.refAt(i)
		pairs[i] = pair{d: euclidean(v, vec), fraud: fraud}
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
