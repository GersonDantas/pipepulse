package main

import (
	"log"

	"pipeline-notifier/internal/handlers"
	"pipeline-notifier/internal/queue"

	"github.com/gin-gonic/gin"
)

func main() {
	queue.StartWorker()

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.POST("/webhook/github", handlers.GithubWebhookHandler)

	log.Println("🚀 Server running on :3000")
	if err := router.Run(":3000"); err != nil {
		log.Fatal(err)
	}
}
