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

	router.GET("/health", handlers.HealthCheckHandler)
	router.POST("/webhook/github", handlers.GithubWebhookHandler)
	router.GET("/pipelines/:id", handlers.GetPipelineStateHandler)

	log.Println("🚀 Server running on :3000")
	if err := router.Run(":3000"); err != nil {
		log.Fatal(err)
	}
}
