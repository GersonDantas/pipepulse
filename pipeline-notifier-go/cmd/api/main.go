package main

import (
	"log"

	"pipeline-notifier/internal/queue"
	internalRouter "pipeline-notifier/internal/router"
)

func main() {
	queue.StartWorker()

	router := internalRouter.SetupRouter()

	log.Println("🚀 Server running on :3000")
	if err := router.Run(":3000"); err != nil {
		log.Fatal(err)
	}
}
