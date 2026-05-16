package services

import (
	"errors"
	"fmt"
	"time"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/queue"
)

var ErrInvalidTimestamp = errors.New("invalid timestamp")

var enqueueFn = queue.Enqueue

const normalizedTimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func HandleWebhook(payload models.GithubWebhookPayload) error {
	timestamp, err := normalizeTimestamp(payload.WorkflowRun.UpdatedAt)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTimestamp, err)
	}

	event := models.Event{
		EventID:    fmt.Sprintf("%d", payload.WorkflowRun.ID),
		PipelineID: fmt.Sprintf("%d", payload.WorkflowRun.ID),
		Status:     getStatus(payload.WorkflowRun),
		Timestamp:  timestamp,
	}

	fmt.Println("📩 Evento recebido:", event)

	enqueueFn(event)

	return nil
}

func getStatus(wr models.GithubWorkflowRun) string {
	if wr.Conclusion == nil || *wr.Conclusion == "" {
		return "running"
	}
	return *wr.Conclusion
}

func normalizeTimestamp(value string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", err
	}

	return parsed.UTC().Format(normalizedTimestampLayout), nil
}
