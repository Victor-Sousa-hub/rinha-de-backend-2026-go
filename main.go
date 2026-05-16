package main

import (
	_ "embed"
	"log"
	"net/http"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/handler"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed resources/references.json.gz
var refsGZ []byte

func main() {
	knn, err := scoring.NewKNN(refsGZ, 5)
	if err != nil {
		log.Fatalf("erro ao carregar referências KNN: %v", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	fraud := handler.NewFraudHandler(knn)
	r.Post("/fraud-score", fraud.Score)

	log.Println("servidor rodando em :9999")
	log.Fatal(http.ListenAndServe(":9999", r))
}
