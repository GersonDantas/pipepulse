package services

import (
	"fmt"
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/queue"
)

func HandleWebhook(payload map[string]interface{}) error {
	wr := payload["workflow_run"].(map[string]interface{})

	event := models.Event{
		EventID:    fmt.Sprintf("%v", wr["id"]),
		PipelineID: fmt.Sprintf("%v", wr["id"]),
		Status:     getStatus(wr),
		Timestamp:  fmt.Sprintf("%v", wr["updated_at"]),
	}

	fmt.Println("📩 Evento recebido:", event)

	queue.Enqueue(event)

	return nil
}

func getStatus(wr map[string]interface{}) string {
	if wr["conclusion"] == nil {
		return "running"
	}
	return wr["conclusion"].(string)
}
