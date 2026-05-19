package handler

import (
	"net/http"

	"github.com/bytedance/sonic"
)

func respond(w http.ResponseWriter, status int, body any) {
	data, err := sonic.Marshal(body)
	if err != nil {
		http.Error(w, "erro interno", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

