package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pipeline-notifier/internal/handlers"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type fakeService struct{}

func (fakeService) Handle(context.Context, services.WebhookRequest) error {
	return nil
}

func TestSetupRouterHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := handlers.New(fakeService{}, repository.NewMemoryStateRepository(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := SetupRouter(handler, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestSetupRouterEmitsJSONAccessLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output strings.Builder
	originalWriter := gin.DefaultWriter
	gin.DefaultWriter = &output
	t.Cleanup(func() {
		gin.DefaultWriter = originalWriter
	})

	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := handlers.New(fakeService{}, repository.NewMemoryStateRepository(), logger)
	router := SetupRouter(handler, logger)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected an access log line")
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("access log = %q, want JSON", line)
		}
	}
}
