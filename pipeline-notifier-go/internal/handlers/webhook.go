package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"pipeline-notifier/internal/services"
)

func GithubWebhookHandler(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}

	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	err = services.HandleWebhook(payload)
	if err != nil {
		log.Println(err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
