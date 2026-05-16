package main

import (
	"log"
	"net/http"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/handler"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	fraud := handler.NewFraudHandler()
	r.Post("/fraud-score", fraud.Score)

	log.Println("servidor rodando em :9999")
	log.Fatal(http.ListenAndServe(":9999", r))
}
