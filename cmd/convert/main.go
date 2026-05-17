// Conversor references.json.gz → resources/references.bin
//
// Formato de saída (flat binary, little-endian):
//
//	[8 bytes]  uint64  — número de registros N
//	[N × 113 bytes]    — registros
//	  [112 bytes]  [14]float64  — vetor de features
//	  [1 byte]     uint8        — label: 1=fraud, 0=legit
//
// Ler bytes diretamente é ~50× mais rápido do que fazer gzip + json.Decode,
// que é o gargalo atual de ~2 minutos no startup da API.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"os"
	"time"
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

	out, err := os.Create("resources/references.bin")
	if err != nil {
		log.Fatalf("criar references.bin: %v", err)
	}
	defer out.Close()

	// Buffer de 4 MB para minimizar syscalls de escrita.
	w := bufio.NewWriterSize(out, 4<<20)

	var hdr [8]byte
	binary.LittleEndian.PutUint64(hdr[:], uint64(len(refs)))
	w.Write(hdr[:])

	var rec [113]byte
	for _, r := range refs {
		for i, f := range r.Vector {
			binary.LittleEndian.PutUint64(rec[i*8:], math.Float64bits(f))
		}
		if r.Label == "fraud" {
			rec[112] = 1
		} else {
			rec[112] = 0
		}
		w.Write(rec[:])
	}

	if err := w.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

	log.Printf("OK: %d registros → resources/references.bin em %s", len(refs), time.Since(start))
}
