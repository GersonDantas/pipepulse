package services

import (
	"context"
	"errors"
	"testing"

	"pipeline-notifier/internal/models"
)

type fakeEventEnqueuer struct {
	events []models.Event
	err    error
}

func (queue *fakeEventEnqueuer) Enqueue(_ context.Context, event models.Event) error {
	if queue.err != nil {
		return queue.err
	}
	queue.events = append(queue.events, event)
	return nil
}

func TestWebhookServiceEnqueuesNormalizedWorkflowRunEvent(t *testing.T) {
	queue := &fakeEventEnqueuer{}
	service := NewWebhookService(queue)
	conclusion := "failure"

	err := service.Handle(context.Background(), "delivery-1", models.GithubWebhookPayload{
		Repository: models.GithubRepository{ID: 10},
		WorkflowRun: models.GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 1,
			Status:     "completed",
			Conclusion: &conclusion,
			UpdatedAt:  "2026-05-16T12:00:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if len(queue.events) != 1 {
		t.Fatalf("events = %d, want 1", len(queue.events))
	}
	if queue.events[0].DeliveryID != "delivery-1" {
		t.Fatalf("DeliveryID = %q, want delivery-1", queue.events[0].DeliveryID)
	}
	if queue.events[0].Status != models.PipelineStatusFailed {
		t.Fatalf("Status = %q, want failed", queue.events[0].Status)
	}
}

func TestWebhookServiceReturnsInvalidTimestampError(t *testing.T) {
	service := NewWebhookService(&fakeEventEnqueuer{})

	err := service.Handle(context.Background(), "delivery-1", models.GithubWebhookPayload{
		Repository: models.GithubRepository{ID: 10},
		WorkflowRun: models.GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 1,
			Status:     "in_progress",
			UpdatedAt:  "invalid",
		},
	})
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("Handle() error = %v, want ErrInvalidTimestamp", err)
	}
}

func TestWebhookServiceReturnsInvalidStatusError(t *testing.T) {
	service := NewWebhookService(&fakeEventEnqueuer{})
	conclusion := "not-real"

	err := service.Handle(context.Background(), "delivery-1", models.GithubWebhookPayload{
		Repository: models.GithubRepository{ID: 10},
		WorkflowRun: models.GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 1,
			Status:     "completed",
			Conclusion: &conclusion,
			UpdatedAt:  "2026-05-16T12:00:00Z",
		},
	})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("Handle() error = %v, want ErrInvalidStatus", err)
	}
}

func TestWebhookServiceReturnsQueueUnavailableError(t *testing.T) {
	service := NewWebhookService(&fakeEventEnqueuer{err: errors.New("full")})

	err := service.Handle(context.Background(), "delivery-1", models.GithubWebhookPayload{
		Repository: models.GithubRepository{ID: 10},
		WorkflowRun: models.GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 1,
			Status:     "in_progress",
			UpdatedAt:  "2026-05-16T12:00:00Z",
		},
	})
	if !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("Handle() error = %v, want ErrQueueUnavailable", err)
	}
}
