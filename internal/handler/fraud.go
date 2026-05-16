package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
)

type FraudHandler struct {
	knn *scoring.KNN
}

func NewFraudHandler(knn *scoring.KNN) *FraudHandler {
	return &FraudHandler{knn: knn}
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

	v := scoring.Vectorize(&req)
	score := h.knn.Score(v)

	respond(w, http.StatusOK, model.FraudScoreResponse{
		Approved:   score < 0.7,
		FraudScore: score,
	})
}
