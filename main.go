package main

import (
	_ "embed"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/config"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/handler"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/logger"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
	"github.com/go-chi/chi/v5"
)

// A diretiva embed só alcança subdiretórios do pacote onde está declarada.
// resources/ fica na raiz do módulo (junto a main.go), então os embeds vivem
// aqui — pacotes em internal/ não conseguem referenciar "../../resources".
//
//go:embed resources/references.bin
var refsBin []byte

//go:embed resources/mcc_risk.json
var mccRiskJSON []byte

func main() {
	cfg := config.Load()

	if err := scoring.LoadMCCRisk(mccRiskJSON); err != nil {
		log.Fatalf("erro ao carregar mcc_risk.json: %v", err)
	}

	knn, err := scoring.NewKNN(refsBin, 5, cfg.Nprobe)
	if err != nil {
		log.Fatalf("erro ao carregar referências KNN: %v", err)
	}
	// refsBin (171 MB) não é mais necessário após a decodificação em NewKNN.
	// Nulificar aqui permite ao GC coletar o slice antes das primeiras requisições.
	refsBin = nil
	runtime.GC()

	r := chi.NewRouter()
	//r.Use(logger.Middleware)
	//r.Use(middleware.Recoverer)

	fraud := handler.NewFraudHandler(knn, cfg.Workers)
	r.Get("/ready", fraud.Ready)
	r.Post("/fraud-score", fraud.Score)

	logger.Startup(cfg.Addr, cfg.Workers)

	if strings.HasPrefix(cfg.Addr, "/") {
		// UDS mode: nginx conecta via socket file, eliminando o stack TCP/IP.
		// Removemos o arquivo antes de criar o listener — sockets antigos causam EADDRINUSE.
		os.Remove(cfg.Addr)
		ln, err := net.Listen("unix", cfg.Addr)
		if err != nil {
			log.Fatalf("falha ao criar socket UDS %s: %v", cfg.Addr, err)
		}
		// nginx worker roda como usuário "nginx" (não root). Sem 0666 o worker
		// não tem permissão de escrita no socket e retorna 502 imediatamente.
		os.Chmod(cfg.Addr, 0666)
		// O HEALTHCHECK no Dockerfile usa wget em localhost:9999 (TCP).
		// Subimos um listener TCP mínimo só para ele não quebrar.
		go func() { log.Fatal(http.ListenAndServe(":9999", r)) }()
		log.Fatal(http.Serve(ln, r))
	} else {
		log.Fatal(http.ListenAndServe(cfg.Addr, r))
	}
}
