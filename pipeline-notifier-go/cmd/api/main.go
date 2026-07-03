package main

import (
	"log"
	"os"

	"pipeline-notifier/internal/queue"
	internalRouter "pipeline-notifier/internal/router"
)

func main() {
	queue.StartWorker()
	port := os.Getenv("port")
	if port == "" {
		port = "3000"
	}

	router := internalRouter.SetupRouter()

	log.Println("🚀 Server running on :" + port)
	if err := router.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
