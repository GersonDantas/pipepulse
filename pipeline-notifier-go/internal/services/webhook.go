package services

import (
	"context"
	"errors"
	"fmt"

	"pipeline-notifier/internal/models"
)

var ErrInvalidTimestamp = errors.New("invalid timestamp")
var ErrInvalidStatus = errors.New("invalid status")
var ErrQueueUnavailable = errors.New("event queue unavailable")

type EventEnqueuer interface {
	Enqueue(context.Context, models.Event) error
}

type WebhookService struct {
	queue EventEnqueuer
}

func NewWebhookService(queue EventEnqueuer) *WebhookService {
	return &WebhookService{queue: queue}
}

func (service *WebhookService) Handle(ctx context.Context, deliveryID string, payload models.GithubWebhookPayload) error {
	event, err := models.NewWorkflowRunEvent(deliveryID, payload)
	if err != nil {
		if errors.Is(err, models.ErrInvalidEventTimestamp) {
			return fmt.Errorf("%w: %v", ErrInvalidTimestamp, err)
		}
		return fmt.Errorf("%w: %v", ErrInvalidStatus, err)
	}

	if err := service.queue.Enqueue(ctx, event); err != nil {
		return fmt.Errorf("%w: %v", ErrQueueUnavailable, err)
	}
	return nil
}
