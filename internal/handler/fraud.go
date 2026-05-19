package handler

import (
	"io"
	"net/http"
	"sync/atomic"

	"github.com/bytedance/sonic"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/logger"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/model"
	"github.com/Victor-Sousa-hub/rinha-de-backend-2026-go/internal/scoring"
)

type job struct {
	id     string
	vector [14]float64
	done   chan float64
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

// NewFraudHandler cria o pool e já dispara as goroutines workers.
// As goroutines ficam vivas para sempre lendo da queue — Go não tem thread pool
// nativo, então criamos o nosso: número fixo de goroutines em loop infinito.
func NewFraudHandler(knn *scoring.KNN, workers, queueCap int) *FraudHandler {
	h := &FraudHandler{
		knn:      knn,
		queue:    make(chan job, queueCap), // buffer = queueCap slots antes de bloquear
		workers:  workers,
		queueCap: queueCap,
	}
	for range workers {
		go h.runWorker()
	}
	return h
}

// runWorker é o loop de cada goroutine do pool.
// `for j := range h.queue` bloqueia enquanto a fila está vazia e para
// automaticamente se o canal for fechado (não acontece aqui, mas é o padrão Go).
// Sem logging no hot path — 900 req/s implicariam ~1800 formatações ANSI/s.
func (h *FraudHandler) runWorker() {
	for j := range h.queue {
		h.active.Add(1)
		j.done <- h.knn.Score(j.vector)
		h.active.Add(-1)
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

	j := job{
		id:     req.ID,
		vector: scoring.Vectorize(&req),
		done:   make(chan float64, 1),
	}

	// select não-bloqueante: se a fila estiver cheia, o caso default executa
	// imediatamente e o cliente recebe 503 em vez de ficar pendurado.
	select {
	case h.queue <- j:
	default:
		logger.Reject(req.ID, h.queueCap)
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "serviço sobrecarregado"})
		return
	}

	// Bloqueia até o worker sinalizar que terminou via j.done (canal com buffer 1).
	score := <-j.done

	respond(w, http.StatusOK, model.FraudScoreResponse{
		Approved:   score < 0.7,
		FraudScore: score,
	})
}
