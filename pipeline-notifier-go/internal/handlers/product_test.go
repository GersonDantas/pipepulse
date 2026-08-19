package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type fakeProductService struct {
	createError error
}

func (service *fakeProductService) Bootstrap(principal repository.Principal) (services.Bootstrap, error) {
	var result services.Bootstrap
	result.Plan = principal.PlanCode
	result.Workspace.ID = principal.WorkspaceID
	return result, nil
}

func (service *fakeProductService) CreateRepository(context.Context, repository.Principal, repository.CreateRepositoryInput) (repository.ProductRepository, error) {
	return repository.ProductRepository{ID: "repository-1", WebhookSecret: "secret"}, service.createError
}

func (service *fakeProductService) ListRepositories(context.Context, repository.Principal) ([]repository.ProductRepository, error) {
	return []repository.ProductRepository{}, nil
}

func (service *fakeProductService) GetRepository(context.Context, repository.Principal, string) (repository.ProductRepository, error) {
	return repository.ProductRepository{ID: "repository-1"}, nil
}

func (service *fakeProductService) RotateRepositorySecret(context.Context, repository.Principal, string) (services.WebhookCredentials, error) {
	return services.WebhookCredentials{WebhookSecret: "rotated"}, nil
}

func (service *fakeProductService) DeleteRepository(context.Context, repository.Principal, string) error {
	return nil
}

func (service *fakeProductService) ListFailures(context.Context, repository.Principal, string, int) (services.FailurePage, error) {
	return services.FailurePage{Failures: []repository.Failure{}}, nil
}

func (service *fakeProductService) PutDevice(context.Context, repository.Principal, string, string) error {
	return nil
}

func (service *fakeProductService) DeleteDevice(context.Context, repository.Principal) error {
	return nil
}
func (service *fakeProductService) DeleteAccount(context.Context, repository.Principal) error {
	return nil
}

func TestProductHandlerExposesAuthenticatedProductAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeProductService{}
	handler := NewProduct(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, repository.Principal{UserID: "user-1", WorkspaceID: "workspace-1", PlanCode: "free"})
	})
	registerProductTestRoutes(router, handler)

	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/v1/bootstrap", "", http.StatusOK},
		{http.MethodPost, "/v1/repositories", `{"github_repository_id":10,"owner":"acme","name":"api","workflow":{"github_workflow_id":20,"name":"CI"}}`, http.StatusCreated},
		{http.MethodGet, "/v1/repositories", "", http.StatusOK},
		{http.MethodGet, "/v1/repositories/repository-1", "", http.StatusOK},
		{http.MethodPost, "/v1/repositories/repository-1/rotate-webhook-secret", "", http.StatusOK},
		{http.MethodDelete, "/v1/repositories/repository-1", "", http.StatusNoContent},
		{http.MethodGet, "/v1/failures?limit=20", "", http.StatusOK},
		{http.MethodPut, "/v1/device", `{"token":"fcm-token","platform":"android"}`, http.StatusNoContent},
		{http.MethodDelete, "/v1/device", "", http.StatusNoContent},
		{http.MethodDelete, "/v1/account", "", http.StatusNoContent},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Fatalf("%s %s = %d %s, want %d", test.method, test.path, recorder.Code, recorder.Body.String(), test.status)
		}
	}

	service.createError = repository.ErrRepositoryLimit
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/repositories", strings.NewReader(`{"github_repository_id":40,"owner":"acme","name":"fourth","workflow":{"github_workflow_id":40,"name":"CI"}}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"feature":"max_repositories"`) {
		t.Fatalf("repository limit response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func registerProductTestRoutes(router *gin.Engine, handler *ProductHandler) {
	router.GET("/v1/bootstrap", handler.Bootstrap)
	router.POST("/v1/repositories", handler.CreateRepository)
	router.GET("/v1/repositories", handler.ListRepositories)
	router.GET("/v1/repositories/:id", handler.GetRepository)
	router.POST("/v1/repositories/:id/rotate-webhook-secret", handler.RotateRepositorySecret)
	router.DELETE("/v1/repositories/:id", handler.DeleteRepository)
	router.GET("/v1/failures", handler.ListFailures)
	router.PUT("/v1/device", handler.PutDevice)
	router.DELETE("/v1/device", handler.DeleteDevice)
	router.DELETE("/v1/account", handler.DeleteAccount)
}
