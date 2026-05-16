package services

import (
	"fmt"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/queue"
)

func HandleWebhook(payload models.GithubWebhookPayload) error {
	event := models.Event{
		EventID:    fmt.Sprintf("%d", payload.WorkflowRun.ID),
		PipelineID: fmt.Sprintf("%d", payload.WorkflowRun.ID),
		Status:     getStatus(payload.WorkflowRun),
		Timestamp:  payload.WorkflowRun.UpdatedAt,
	}

	fmt.Println("📩 Evento recebido:", event)

	queue.Enqueue(event)

	return nil
}

func getStatus(wr models.GithubWorkflowRun) string {
	if wr.Conclusion == nil || *wr.Conclusion == "" {
		return "running"
	}
	return *wr.Conclusion
}
