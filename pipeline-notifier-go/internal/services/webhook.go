package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

var ErrInvalidTimestamp = errors.New("invalid timestamp")
var ErrInvalidStatus = errors.New("invalid status")
var ErrInvalidPayload = errors.New("invalid payload")
var ErrInvalidSignature = errors.New("invalid signature")
var ErrRepositoryMismatch = errors.New("repository mismatch")
var ErrWebhookEndpointNotFound = errors.New("webhook endpoint not found")
var ErrMonitoredWorkflowNotFound = errors.New("monitored workflow not found")
var ErrQueueUnavailable = errors.New("event queue unavailable")

type WebhookRequest struct {
	EndpointID string
	DeliveryID string
	Signature  string
	Body       []byte
}

type EventEnqueuer interface {
	Enqueue(context.Context, models.Event) error
}

type WebhookRepository interface {
	GetWebhookEndpoint(context.Context, string) (*repository.WebhookEndpoint, error)
	GetMonitoredWorkflow(context.Context, string, int64) (string, error)
}

type WebhookService struct {
	queue      EventEnqueuer
	repository WebhookRepository
	secretBox  *secrets.Box
}

func NewWebhookService(queue EventEnqueuer, webhookRepository WebhookRepository, secretBox *secrets.Box) *WebhookService {
	return &WebhookService{queue: queue, repository: webhookRepository, secretBox: secretBox}
}

func (service *WebhookService) Handle(ctx context.Context, request WebhookRequest) error {
	endpoint, err := service.repository.GetWebhookEndpoint(ctx, request.EndpointID)
	if errors.Is(err, repository.ErrWebhookEndpointNotFound) {
		return ErrWebhookEndpointNotFound
	}
	if err != nil {
		return fmt.Errorf("load webhook endpoint: %w", err)
	}

	secret, err := service.secretBox.Decrypt(endpoint.WebhookSecretCiphertext)
	if err != nil {
		return fmt.Errorf("decrypt webhook secret: %w", err)
	}
	defer clear(secret)
	if !validSignature(secret, request.Body, request.Signature) {
		return ErrInvalidSignature
	}

	var payload models.GithubWebhookPayload
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		return ErrInvalidPayload
	}
	event, err := models.NewWorkflowRunEvent(request.DeliveryID, payload)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrInvalidEventTimestamp):
			return fmt.Errorf("%w: %v", ErrInvalidTimestamp, err)
		case errors.Is(err, models.ErrInvalidWorkflowRun), errors.Is(err, models.ErrInvalidDeliveryID):
			return fmt.Errorf("%w: %v", ErrInvalidPayload, err)
		default:
			return fmt.Errorf("%w: %v", ErrInvalidStatus, err)
		}
	}
	if event.RepositoryID != endpoint.GithubRepositoryID {
		return ErrRepositoryMismatch
	}
	workflowID, err := service.repository.GetMonitoredWorkflow(ctx, endpoint.RepositoryID, event.WorkflowID)
	if errors.Is(err, repository.ErrMonitoredWorkflowNotFound) {
		return ErrMonitoredWorkflowNotFound
	}
	if err != nil {
		return fmt.Errorf("load monitored workflow: %w", err)
	}
	event.RepositoryRecordID = endpoint.RepositoryID
	event.MonitoredWorkflowID = workflowID

	if err := service.queue.Enqueue(ctx, event); err != nil {
		return fmt.Errorf("%w: %v", ErrQueueUnavailable, err)
	}
	return nil
}

func validSignature(secret, body []byte, signature string) bool {
	encoded, ok := strings.CutPrefix(signature, "sha256=")
	if !ok {
		return false
	}
	provided, err := hex.DecodeString(encoded)
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write(body)
	return hmac.Equal(digest.Sum(nil), provided)
}
