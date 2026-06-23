package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/queue"
	"pipeline-notifier/internal/repository"

	"github.com/gin-gonic/gin"
)

func TestGithubWebhookHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	queue.StartWorker()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name: "valid payload",
			body: `{
				"workflow_run": {
					"id": 123,
					"conclusion": "success",
					"updated_at": "2026-05-16T12:00:00Z"
				}
			}`,
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "invalid json",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing workflow_run",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "id as string",
			body: `{
				"workflow_run": {
					"id": "123",
					"conclusion": "success",
					"updated_at": "2026-05-16T12:00:00Z"
				}
			}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid timestamp",
			body: `{
				"workflow_run": {
					"id": 123,
					"conclusion": "success",
					"updated_at": "16-05-2026 12:00:00"
				}
			}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid status",
			body: `{
				"workflow_run": {
					"id": 123,
					"conclusion": "cancelled",
					"updated_at": "2026-05-16T12:00:00Z"
				}
			}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/webhook/github", GithubWebhookHandler)

			req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestGetPipelineStateHandlerReturnsState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository.Reset()

	repository.SaveState(repository.State{
		PipelineID:  "123",
		Status:      "failed",
		Timestamp:   "2026-05-16T15:00:00.000000000Z",
		LastEventID: "123",
	})

	router := gin.New()
	router.GET("/pipelines/:id", GetPipelineStateHandler)

	req := httptest.NewRequest(http.MethodGet, "/pipelines/123", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		PipelineID  string                `json:"pipeline_id"`
		Status      models.PipelineStatus `json:"status"`
		Timestamp   string                `json:"timestamp"`
		LastEventID string                `json:"last_event_id"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Response body is not valid: %v", err)
	}

	if body.PipelineID != "123" {
		t.Fatalf("pipeline_id = %q, want %q", body.PipelineID, "123")
	}

	if body.Timestamp != "2026-05-16T15:00:00.000000000Z" {
		t.Fatalf("timestamp = %q, want %q", body.Timestamp, "2026-05-16T15:00:00.000000000Z")
	}

	if body.Status != models.PipelineStatusFailed {
		t.Fatalf("status = %q, want %q", body.Status, "failed")
	}

	if body.LastEventID != "123" {
		t.Fatalf("last_event_id = %q, want %q", body.LastEventID, "123")
	}
}

func TestGetPipelineStateHandlerReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository.Reset()

	router := gin.New()
	router.GET("/pipelines/:id", GetPipelineStateHandler)

	req := httptest.NewRequest(http.MethodGet, "/pipelines/999", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHealthCheckHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/health", HealthCheckHandler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := struct {
		Status string `json:"status"`
	}{}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %q, want %q", rec.Body.String(), `{"status":"ok"}`)
	}

	if body.Status != "ok" {
		t.Fatalf("status = %q, want %q", body.Status, "ok")
	}
}
