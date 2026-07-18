package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"

	"github.com/gin-gonic/gin"
)

type fakeWorkflowRunService struct {
	called     bool
	deliveryID string
	err        error
}

func (service *fakeWorkflowRunService) Handle(_ context.Context, deliveryID string, _ models.GithubWebhookPayload) error {
	service.called = true
	service.deliveryID = deliveryID
	return service.err
}

func newTestHandler(service WorkflowRunService, stateRepository repository.StateRepository) *Handler {
	return New(service, stateRepository, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func validWebhookBody() string {
	return `{
		"repository": {"id": 10},
		"workflow_run": {
			"id": 20,
			"workflow_id": 30,
			"run_attempt": 1,
			"status": "completed",
			"conclusion": "success",
			"updated_at": "2026-05-16T12:00:00Z"
		}
	}`
}

func TestGithubWebhookAcceptsValidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeWorkflowRunService{}
	handler := newTestHandler(service, repository.NewMemoryStateRepository())
	router := gin.New()
	router.POST("/webhook/github", handler.GithubWebhook)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(validWebhookBody()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256=test")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if !service.called || service.deliveryID != "delivery-1" {
		t.Fatalf("service call = %#v, want delivery-1", service)
	}
}

func TestGithubWebhookRejectsMissingRequiredHeadersAndInvalidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := newTestHandler(&fakeWorkflowRunService{}, repository.NewMemoryStateRepository())

	tests := []struct {
		name       string
		body       string
		signature  string
		deliveryID string
		wantStatus int
	}{
		{name: "missing signature", body: validWebhookBody(), wantStatus: http.StatusUnauthorized},
		{name: "missing delivery", body: validWebhookBody(), signature: "sha256=test", wantStatus: http.StatusBadRequest},
		{name: "invalid payload", body: `{`, signature: "sha256=test", deliveryID: "delivery-1", wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/webhook/github", handler.GithubWebhook)
			req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Hub-Signature-256", test.signature)
			req.Header.Set("X-GitHub-Delivery", test.deliveryID)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, test.wantStatus)
			}
		})
	}
}

func TestGetPipelineStateReturnsState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stateRepository := repository.NewMemoryStateRepository()
	stateRepository.SaveState(repository.State{
		PipelineID:     "10:30",
		RepositoryID:   10,
		WorkflowID:     30,
		WorkflowRunID:  20,
		RunAttempt:     1,
		Status:         models.PipelineStatusFailed,
		Timestamp:      time.Date(2026, time.May, 16, 15, 0, 0, 0, time.UTC),
		LastDeliveryID: "delivery-1",
	})
	handler := newTestHandler(&fakeWorkflowRunService{}, stateRepository)
	router := gin.New()
	router.GET("/pipelines/:id", handler.GetPipelineState)

	req := httptest.NewRequest(http.MethodGet, "/pipelines/10:30", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body repository.State
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid: %v", err)
	}
	if body.LastDeliveryID != "delivery-1" || body.Status != models.PipelineStatusFailed {
		t.Fatalf("response body = %#v, want failed state", body)
	}
}

func TestHealthCheckReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := newTestHandler(&fakeWorkflowRunService{}, repository.NewMemoryStateRepository())
	router := gin.New()
	router.GET("/health", handler.HealthCheck)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
