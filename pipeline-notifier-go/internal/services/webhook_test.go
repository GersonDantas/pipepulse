package services

import (
	"errors"
	"testing"

	"pipeline-notifier/internal/models"
)

func captureEnqueuedEvent(t *testing.T) *models.Event {
	t.Helper()

	original := enqueueFn
	var captured models.Event

	enqueueFn = func(event models.Event) {
		captured = event
	}

	t.Cleanup(func() {
		enqueueFn = original
	})

	return &captured
}

func TestHandleWebhookNormalizesTimestampToUTC(t *testing.T) {
	captured := captureEnqueuedEvent(t)
	conclusion := "success"

	payload := models.GithubWebhookPayload{
		WorkflowRun: models.GithubWorkflowRun{
			ID:         123,
			Conclusion: &conclusion,
			UpdatedAt:  "2026-05-16T12:00:00-03:00",
		},
	}

	if err := HandleWebhook(payload); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	if captured.Timestamp != "2026-05-16T15:00:00.000000000Z" {
		t.Fatalf("timestamp = %q, want normalized UTC timestamp", captured.Timestamp)
	}
	if captured.Status != "success" {
		t.Fatalf("status = %q, want success", captured.Status)
	}
	if captured.EventID != "123" {
		t.Fatalf("event id = %q, want 123", captured.EventID)
	}
}

func TestHandleWebhookUsesRunningWhenConclusionIsEmpty(t *testing.T) {
	captured := captureEnqueuedEvent(t)

	payload := models.GithubWebhookPayload{
		WorkflowRun: models.GithubWorkflowRun{
			ID:        123,
			UpdatedAt: "2026-05-16T12:00:00Z",
		},
	}

	if err := HandleWebhook(payload); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	if captured.Status != "running" {
		t.Fatalf("status = %q, want running", captured.Status)
	}
	if captured.Timestamp != "2026-05-16T12:00:00.000000000Z" {
		t.Fatalf("timestamp = %q, want normalized UTC timestamp", captured.Timestamp)
	}
}

func TestHandleWebhookReturnsInvalidTimestampError(t *testing.T) {
	captureEnqueuedEvent(t)

	payload := models.GithubWebhookPayload{
		WorkflowRun: models.GithubWorkflowRun{
			ID:        123,
			UpdatedAt: "16-05-2026 12:00:00",
		},
	}

	err := HandleWebhook(payload)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("error = %v, want ErrInvalidTimestamp", err)
	}
}
