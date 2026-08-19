package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

const validBody = `{
	"repository":{"id":10},
	"workflow_run":{"id":20,"workflow_id":30,"run_attempt":1,"status":"completed","conclusion":"failure","updated_at":"2026-05-16T12:00:00Z"}
}`

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

type fakeWebhookRepository struct {
	endpoint    *repository.WebhookEndpoint
	workflowID  string
	endpointErr error
	workflowErr error
}

func (store *fakeWebhookRepository) GetWebhookEndpoint(context.Context, string) (*repository.WebhookEndpoint, error) {
	return store.endpoint, store.endpointErr
}

func (store *fakeWebhookRepository) GetMonitoredWorkflow(context.Context, string, int64) (string, error) {
	if store.workflowErr != nil {
		return "", store.workflowErr
	}
	return store.workflowID, nil
}

func newWebhookService(t *testing.T, queue *fakeEventEnqueuer, store *fakeWebhookRepository) (*WebhookService, []byte) {
	t.Helper()
	secret := []byte("repository-webhook-secret")
	box, err := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	ciphertext, err := box.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if store.endpoint == nil && store.endpointErr == nil {
		store.endpoint = &repository.WebhookEndpoint{
			RepositoryID:            "repository-id",
			GithubRepositoryID:      10,
			WebhookSecretCiphertext: ciphertext,
		}
	}
	if store.workflowID == "" && store.workflowErr == nil {
		store.workflowID = "workflow-id"
	}
	return NewWebhookService(queue, store, box), secret
}

func signedRequest(secret []byte, body string) WebhookRequest {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(body))
	return WebhookRequest{
		EndpointID: "endpoint-id",
		DeliveryID: "delivery-1",
		Signature:  "sha256=" + hex.EncodeToString(digest.Sum(nil)),
		Body:       []byte(body),
	}
}

func TestWebhookServiceVerifiesAndAssociatesWorkflowRun(t *testing.T) {
	queue := &fakeEventEnqueuer{}
	service, secret := newWebhookService(t, queue, &fakeWebhookRepository{})

	if err := service.Handle(context.Background(), signedRequest(secret, validBody)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if len(queue.events) != 1 {
		t.Fatalf("events = %d, want 1", len(queue.events))
	}
	event := queue.events[0]
	if event.DeliveryID != "delivery-1" || event.Status != models.PipelineStatusFailed {
		t.Fatalf("event = %#v, want normalized failed delivery", event)
	}
	if event.RepositoryRecordID != "repository-id" || event.MonitoredWorkflowID != "workflow-id" {
		t.Fatalf("event association = %q/%q, want repository-id/workflow-id", event.RepositoryRecordID, event.MonitoredWorkflowID)
	}
}

func TestWebhookServiceRejectsUntrustedRequestsBeforePersistence(t *testing.T) {
	tests := []struct {
		name      string
		store     *fakeWebhookRepository
		request   func([]byte) WebhookRequest
		wantError error
	}{
		{name: "unknown endpoint", store: &fakeWebhookRepository{endpointErr: repository.ErrWebhookEndpointNotFound}, request: func(secret []byte) WebhookRequest { return signedRequest(secret, validBody) }, wantError: ErrWebhookEndpointNotFound},
		{name: "invalid signature", store: &fakeWebhookRepository{}, request: func([]byte) WebhookRequest { return signedRequest([]byte("wrong-secret"), validBody) }, wantError: ErrInvalidSignature},
		{name: "repository mismatch", store: &fakeWebhookRepository{}, request: func(secret []byte) WebhookRequest {
			return signedRequest(secret, `{"repository":{"id":99},"workflow_run":{"id":20,"workflow_id":30,"run_attempt":1,"status":"completed","conclusion":"failure","updated_at":"2026-05-16T12:00:00Z"}}`)
		}, wantError: ErrRepositoryMismatch},
		{name: "workflow not monitored", store: &fakeWebhookRepository{workflowErr: repository.ErrMonitoredWorkflowNotFound}, request: func(secret []byte) WebhookRequest { return signedRequest(secret, validBody) }, wantError: ErrMonitoredWorkflowNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := &fakeEventEnqueuer{}
			service, secret := newWebhookService(t, queue, test.store)
			err := service.Handle(context.Background(), test.request(secret))
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Handle() error = %v, want %v", err, test.wantError)
			}
			if len(queue.events) != 0 {
				t.Fatalf("events = %d, want no persisted event", len(queue.events))
			}
		})
	}
}

func TestWebhookServiceClassifiesInvalidPayloadAndQueueFailure(t *testing.T) {
	t.Run("invalid timestamp", func(t *testing.T) {
		queue := &fakeEventEnqueuer{}
		service, secret := newWebhookService(t, queue, &fakeWebhookRepository{})
		body := `{"repository":{"id":10},"workflow_run":{"id":20,"workflow_id":30,"run_attempt":1,"status":"in_progress","updated_at":"invalid"}}`
		if err := service.Handle(context.Background(), signedRequest(secret, body)); !errors.Is(err, ErrInvalidTimestamp) {
			t.Fatalf("Handle() error = %v, want ErrInvalidTimestamp", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		queue := &fakeEventEnqueuer{}
		service, secret := newWebhookService(t, queue, &fakeWebhookRepository{})
		if err := service.Handle(context.Background(), signedRequest(secret, `{`)); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("Handle() error = %v, want ErrInvalidPayload", err)
		}
	})

	t.Run("queue unavailable", func(t *testing.T) {
		queue := &fakeEventEnqueuer{err: errors.New("database unavailable")}
		service, secret := newWebhookService(t, queue, &fakeWebhookRepository{})
		if err := service.Handle(context.Background(), signedRequest(secret, validBody)); !errors.Is(err, ErrQueueUnavailable) {
			t.Fatalf("Handle() error = %v, want ErrQueueUnavailable", err)
		}
	})
}
