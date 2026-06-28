package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetupRouterHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("status body = %q, want %q", body.Status, "ok")
	}
}
