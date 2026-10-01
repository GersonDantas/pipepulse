package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type fakeAuthService struct {
	accessToken string
	principal   repository.Principal
}

func (service *fakeAuthService) Start(context.Context) (services.OAuthStart, error) {
	return services.OAuthStart{AuthorizationURL: "https://github.com/login/oauth/authorize?state=test", ExchangeVerifier: "verifier"}, nil
}

func (service *fakeAuthService) Callback(context.Context, string, string) (string, error) {
	return "com.pipepulse.app://oauth?code=exchange", nil
}

func (service *fakeAuthService) Exchange(context.Context, string, string) (services.Tokens, error) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	return services.Tokens{AccessToken: "access", RefreshToken: "refresh", AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)}, nil
}

func (service *fakeAuthService) Refresh(context.Context, string) (services.Tokens, error) {
	return service.Exchange(context.Background(), "", "")
}

func (service *fakeAuthService) Authenticate(_ context.Context, token string) (repository.Principal, error) {
	service.accessToken = token
	if token != "valid-token" {
		return repository.Principal{}, services.ErrInvalidSession
	}
	return service.principal, nil
}

func (service *fakeAuthService) Logout(_ context.Context, token string) error {
	service.accessToken = token
	return nil
}

func TestAuthHandlerExposesOAuthAndProtectsProductRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeAuthService{principal: repository.Principal{UserID: "user-1", WorkspaceID: "workspace-1", PlanCode: "free"}}
	handler := NewAuth(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := gin.New()
	router.POST("/v1/auth/github/start", handler.GithubStart)
	router.GET("/v1/auth/github/callback", handler.GithubCallback)
	router.POST("/v1/auth/github/exchange", handler.Exchange)
	router.POST("/v1/auth/refresh", handler.Refresh)
	protected := router.Group("/v1", handler.RequireSession())
	protected.GET("/bootstrap", func(c *gin.Context) {
		c.JSON(http.StatusOK, PrincipalFromContext(c))
	})
	protected.POST("/auth/logout", handler.Logout)

	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/v1/auth/github/start", nil))
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), "github.com") || !strings.Contains(start.Body.String(), `"exchange_verifier":"verifier"`) {
		t.Fatalf("start response = %d %s", start.Code, start.Body.String())
	}
	callback := httptest.NewRecorder()
	router.ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback?state=state&code=code", nil))
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != "com.pipepulse.app://oauth?code=exchange" {
		t.Fatalf("callback response = %d Location %q", callback.Code, callback.Header().Get("Location"))
	}
	exchange := httptest.NewRecorder()
	router.ServeHTTP(exchange, httptest.NewRequest(http.MethodPost, "/v1/auth/github/exchange", strings.NewReader(`{"code":"exchange"}`)))
	if exchange.Code != http.StatusBadRequest {
		t.Fatalf("exchange without client verifier = %d %s, want 400", exchange.Code, exchange.Body.String())
	}
	exchange = httptest.NewRecorder()
	router.ServeHTTP(exchange, httptest.NewRequest(http.MethodPost, "/v1/auth/github/exchange", strings.NewReader(`{"code":"exchange","exchange_verifier":"verifier"}`)))
	if exchange.Code != http.StatusOK || !strings.Contains(exchange.Body.String(), `"access_token":"access"`) {
		t.Fatalf("exchange response = %d %s, want tokens", exchange.Code, exchange.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/bootstrap", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.Code)
	}
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/v1/bootstrap", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer valid-token")
	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK || !strings.Contains(authorized.Body.String(), "workspace-1") || service.accessToken != "valid-token" {
		t.Fatalf("authorized response = %d %s token %q", authorized.Code, authorized.Body.String(), service.accessToken)
	}
}
