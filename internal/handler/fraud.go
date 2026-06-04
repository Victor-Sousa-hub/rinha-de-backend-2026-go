package handler

import (
	"io"
	"net/http"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
	"github.com/bytedance/sonic"
)

// FraudHandler usa um semáforo para limitar o paralelismo do KNN sem trocar
// de goroutine. Comparado ao worker pool anterior (3 hops: HTTP→worker→HTTP
// via dois canais), aqui Score() roda direto na goroutine HTTP — 1 semáforo
// acquire/release, zero troca de contexto entre goroutines.
//
// Goroutines HTTP bloqueiam no semáforo se todos os slots estiverem ocupados;
// o scheduler do Go suspende-as sem custo de sistema operacional.
type FraudHandler struct {
	knn     *scoring.KNN
	sem     chan struct{}
	workers int
}

func NewFraudHandler(knn *scoring.KNN, workers int) *FraudHandler {
	return &FraudHandler{
		knn:     knn,
		sem:     make(chan struct{}, workers),
		workers: workers,
	}
}

type readyResponse struct {
	Workers int `json:"workers"`
	Active  int `json:"active"`
}

func (h *FraudHandler) Ready(w http.ResponseWriter, r *http.Request) {
	active := len(h.sem)
	// logger.Ready(active, h.workers)

	status := http.StatusOK
	if active >= h.workers {
		status = http.StatusServiceUnavailable
	}
	respond(w, status, readyResponse{
		Workers: h.workers,
		Active:  active,
	})
}

func (h *FraudHandler) Score(w http.ResponseWriter, r *http.Request) {
	var req model.FraudScoreRequest
	raw, err := io.ReadAll(r.Body)
	if err != nil || sonic.Unmarshal(raw, &req) != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "body inválido"})
		return
	}

	if err := req.Validate(); err != nil {
		errs := err.(model.ValidationErrors)
		respond(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "dados inválidos",
			"fields": []string(errs),
		})
		return
	}

	if req.LastTransaction == nil {
		req.LastTransaction = &model.LastTransaction{KmFromCurrent: -1}
	}

	vec := scoring.Vectorize(&req)

	// Semáforo: bloqueia se todos os slots estiverem ocupados.
	// Erros HTTP (503) são penalizados na detecção — bloquear é melhor que rejeitar.
	h.sem <- struct{}{}
	score := h.knn.Score(vec)
	<-h.sem

	respond(w, http.StatusOK, model.FraudScoreResponse{
		Approved:   score < 0.5,
		FraudScore: score,
	})
}
