package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
)

type FraudHandler struct{}

func NewFraudHandler() *FraudHandler {
	return &FraudHandler{}
}

func (h *FraudHandler) Score(w http.ResponseWriter, r *http.Request) {
	var req model.FraudScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	score := calcFraudScore(&req)

	respond(w, http.StatusOK, model.FraudScoreResponse{
		Approved:   score < 0.7,
		FraudScore: score,
	})
}

func calcFraudScore(req *model.FraudScoreRequest) float64 {
	_ = scoring.Vectorize(req)
	return 0.0 // KNN ainda não implementado
}
