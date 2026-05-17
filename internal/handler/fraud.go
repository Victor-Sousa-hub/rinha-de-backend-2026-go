package handler

import (
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/logger"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
)

type job struct {
	id      string
	vector  [14]float64
	verbose bool
	done    chan jobResult
}

type jobResult struct {
	score     float64
	neighbors []scoring.Neighbor
}

type verboseResponse struct {
	Approved   bool               `json:"approved"`
	FraudScore float64            `json:"fraud_score"`
	Neighbors  []scoring.Neighbor `json:"neighbors"`
}

// FraudHandler implementa o padrão worker pool:
//   - queue é um canal com buffer (tamanho = queueCap) que age como fila de trabalho.
//   - workers goroutines ficam bloqueadas lendo da queue; cada uma processa um job por vez.
//   - active conta quantas goroutines estão calculando no momento (atomic, sem mutex).
//
// Vantagem sobre "uma goroutine por request": limita o paralelismo ao número de
// workers, evitando que n requisições simultâneas saturem o CPU com n KNNs em paralelo.
type FraudHandler struct {
	knn      *scoring.KNN
	queue    chan job
	active   atomic.Int64
	workers  int
	queueCap int
}

func NewFraudHandler(knn *scoring.KNN, workers, queueCap int) *FraudHandler {
	h := &FraudHandler{
		knn:      knn,
		queue:    make(chan job, queueCap),
		workers:  workers,
		queueCap: queueCap,
	}
	for range workers {
		go h.runWorker()
	}
	return h
}

func (h *FraudHandler) runWorker() {
	for j := range h.queue {
		n := h.active.Add(1)
		logger.WorkerIn(j.id, n, len(h.queue))

		var res jobResult
		if j.verbose {
			res.score, res.neighbors = h.knn.ScoreVerbose(j.vector)
		} else {
			res.score = h.knn.Score(j.vector)
		}

		j.done <- res
		n = h.active.Add(-1)
		logger.WorkerOut(j.id, n, len(h.queue))
	}
}

type readyResponse struct {
	Workers  int `json:"workers"`
	QueueCap int `json:"queue_cap"`
	Active   int `json:"active"`
	Queued   int `json:"queued"`
}

func (h *FraudHandler) Ready(w http.ResponseWriter, r *http.Request) {
	queued := len(h.queue)
	active := int(h.active.Load())
	logger.Ready(queued, active, h.queueCap)

	status := http.StatusOK
	if queued >= h.queueCap {
		status = http.StatusServiceUnavailable
	}
	respond(w, status, readyResponse{
		Workers:  h.workers,
		QueueCap: h.queueCap,
		Active:   active,
		Queued:   queued,
	})
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

	vector := scoring.Vectorize(&req)
	verbose := r.URL.Query().Get("verbose") == "true"

	j := job{
		id:      req.ID,
		vector:  vector,
		verbose: verbose,
		done:    make(chan jobResult, 1),
	}

	// select não-bloqueante: se a fila estiver cheia, o caso default executa
	// imediatamente e o cliente recebe 503 em vez de ficar pendurado.
	select {
	case h.queue <- j:
		logger.Enqueue(req.ID, len(h.queue))
	default:
		logger.Reject(req.ID, h.queueCap)
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "serviço sobrecarregado"})
		return
	}

	// Bloqueia até o worker sinalizar que terminou via j.done (canal com buffer 1).
	res := <-j.done

	if verbose {
		respond(w, http.StatusOK, verboseResponse{
			Approved:   res.score < 0.7,
			FraudScore: res.score,
			Neighbors:  res.neighbors,
		})
		return
	}

	respond(w, http.StatusOK, model.FraudScoreResponse{
		Approved:   res.score < 0.7,
		FraudScore: res.score,
	})
}
