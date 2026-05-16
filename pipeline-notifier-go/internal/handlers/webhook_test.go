package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"pipeline-notifier/internal/queue"
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
			wantStatus: http.StatusOK,
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
