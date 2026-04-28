package main

import (
	"log"
	"net/http"

	"pipeline-notifier/internal/handlers"
	"pipeline-notifier/internal/queue"
)

func main() {
	queue.StartWorker()
	mux := http.NewServeMux()

	mux.HandleFunc("/webhook/github", handlers.GithubWebhookHandler)

	log.Println("🚀 Server running on :3000")
	http.ListenAndServe(":3000", mux)
}
