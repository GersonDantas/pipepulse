package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"pipeline-notifier/internal/handlers"
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"

	"github.com/gin-gonic/gin"
)

type fakeService struct{}

func (fakeService) Handle(context.Context, string, models.GithubWebhookPayload) error {
	return nil
}

func TestSetupRouterHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := handlers.New(fakeService{}, repository.NewMemoryStateRepository(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := SetupRouter(handler)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}
