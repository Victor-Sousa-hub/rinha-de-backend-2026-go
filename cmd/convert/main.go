// Conversor references.json.gz → resources/references.bin
//
// Formato de saída (flat binary, little-endian):
//
//	[8 bytes]  uint64  — número de registros N
//	[N × 57 bytes]     — registros
//	  [56 bytes]  [14]float32  — vetor de features (float32 economiza 50% vs float64)
//	  [1 byte]    uint8        — label: 1=fraud, 0=legit
//
// float32 é suficiente para features normalizadas em [0,1] com 7 dígitos de precisão.
// Reduz o arquivo de 324 MB (float64) para ~163 MB, acelerando o build Docker.
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

	w := bufio.NewWriterSize(out, 4<<20)

	var hdr [8]byte
	binary.LittleEndian.PutUint64(hdr[:], uint64(len(refs)))
	w.Write(hdr[:])

	var rec [57]byte // 14×float32 + 1 byte label
	for _, r := range refs {
		for i, f64 := range r.Vector {
			binary.LittleEndian.PutUint32(rec[i*4:], math.Float32bits(float32(f64)))
		}
		if r.Label == "fraud" {
			rec[56] = 1
		} else {
			rec[56] = 0
		}
		w.Write(rec[:])
	}

	if err := w.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

	info, _ := out.Stat()
	log.Printf("OK: %d registros → resources/references.bin (%.0f MB) em %s",
		len(refs), float64(info.Size())/(1<<20), time.Since(start))
}
