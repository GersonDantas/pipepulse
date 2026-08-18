package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type fakeWorkflowRunService struct {
	request services.WebhookRequest
	err     error
}

func (service *fakeWorkflowRunService) Handle(_ context.Context, request services.WebhookRequest) error {
	service.request = request
	return service.err
}

func newTestHandler(service WorkflowRunService, stateRepository repository.StateRepository) *Handler {
	return New(service, stateRepository, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func validWebhookBody() string {
	return `{"repository":{"id":10},"workflow_run":{"id":20,"workflow_id":30,"run_attempt":1,"status":"completed","conclusion":"success","updated_at":"2026-05-16T12:00:00Z"}}`
}

func webhookRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/webhooks/github/endpoint-1", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Hub-Signature-256", "sha256=test")
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-GitHub-Event", "workflow_run")
	return request
}

func TestGithubWebhookPassesRawBodyAndEndpointToService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeWorkflowRunService{}
	router := gin.New()
	router.POST("/webhooks/github/:endpoint_id", newTestHandler(service, repository.NewMemoryStateRepository()).GithubWebhook)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, webhookRequest(validWebhookBody()))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if service.request.EndpointID != "endpoint-1" || service.request.DeliveryID != "delivery-1" || service.request.Signature != "sha256=test" {
		t.Fatalf("service request = %#v, want endpoint and GitHub headers", service.request)
	}
	if string(service.request.Body) != validWebhookBody() {
		t.Fatalf("raw body changed before signature validation")
	}
}

func TestGithubWebhookRejectsInvalidHeadersAndOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		mutate     func(*http.Request)
		body       string
		wantStatus int
	}{
		{name: "missing signature", mutate: func(request *http.Request) { request.Header.Del("X-Hub-Signature-256") }, wantStatus: http.StatusUnauthorized},
		{name: "missing delivery", mutate: func(request *http.Request) { request.Header.Del("X-GitHub-Delivery") }, wantStatus: http.StatusBadRequest},
		{name: "missing event", mutate: func(request *http.Request) { request.Header.Del("X-GitHub-Event") }, wantStatus: http.StatusBadRequest},
		{name: "wrong event", mutate: func(request *http.Request) { request.Header.Set("X-GitHub-Event", "push") }, wantStatus: http.StatusBadRequest},
		{name: "oversized body", body: strings.Repeat("x", maxWebhookBodyBytes+1), wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeWorkflowRunService{}
			router := gin.New()
			router.POST("/webhooks/github/:endpoint_id", newTestHandler(service, repository.NewMemoryStateRepository()).GithubWebhook)
			body := test.body
			if body == "" {
				body = validWebhookBody()
			}
			request := webhookRequest(body)
			if test.mutate != nil {
				test.mutate(request)
			}
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if len(service.request.Body) != 0 {
				t.Fatal("service called for structurally invalid request")
			}
		})
	}
}

func TestGithubWebhookMapsServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		err        error
		wantStatus int
	}{
		{services.ErrInvalidSignature, http.StatusUnauthorized},
		{services.ErrWebhookEndpointNotFound, http.StatusNotFound},
		{services.ErrMonitoredWorkflowNotFound, http.StatusNotFound},
		{services.ErrInvalidPayload, http.StatusBadRequest},
		{services.ErrInvalidTimestamp, http.StatusBadRequest},
		{services.ErrInvalidStatus, http.StatusUnprocessableEntity},
		{services.ErrRepositoryMismatch, http.StatusUnprocessableEntity},
		{services.ErrQueueUnavailable, http.StatusServiceUnavailable},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}

	for _, test := range tests {
		service := &fakeWorkflowRunService{err: test.err}
		router := gin.New()
		router.POST("/webhooks/github/:endpoint_id", newTestHandler(service, repository.NewMemoryStateRepository()).GithubWebhook)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, webhookRequest(validWebhookBody()))
		if recorder.Code != test.wantStatus {
			t.Fatalf("error %v: status = %d, want %d", test.err, recorder.Code, test.wantStatus)
		}
	}
}

func TestGetPipelineStateReturnsState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stateRepository := repository.NewMemoryStateRepository()
	if err := stateRepository.SaveState(context.Background(), repository.State{
		PipelineID: "10:30", RepositoryID: 10, WorkflowID: 30, WorkflowRunID: 20, RunAttempt: 1,
		Status: models.PipelineStatusFailed, Timestamp: time.Date(2026, time.May, 16, 15, 0, 0, 0, time.UTC), LastDeliveryID: "delivery-1",
	}); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	handler := newTestHandler(&fakeWorkflowRunService{}, stateRepository)
	router := gin.New()
	router.GET("/pipelines/:id", handler.GetPipelineState)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pipelines/10:30", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body repository.State
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
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
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}
