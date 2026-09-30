package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

const principalContextKey = "pipepulse_principal"

type AuthenticationService interface {
	Start(context.Context) (services.OAuthStart, error)
	Callback(context.Context, string, string) (string, error)
	Exchange(context.Context, string, string) (services.Tokens, error)
	Refresh(context.Context, string) (services.Tokens, error)
	Authenticate(context.Context, string) (repository.Principal, error)
	Logout(context.Context, string) error
}

type AuthHandler struct {
	service AuthenticationService
	logger  *slog.Logger
}

func NewAuth(service AuthenticationService, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{service: service, logger: logger}
}

func (handler *AuthHandler) GithubStart(c *gin.Context) {
	result, err := handler.service.Start(c.Request.Context())
	if err != nil {
		handler.internalError(c, "start GitHub OAuth", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (handler *AuthHandler) GithubCallback(c *gin.Context) {
	redirect, err := handler.service.Callback(c.Request.Context(), c.Query("state"), c.Query("code"))
	if err != nil {
		if errors.Is(err, services.ErrInvalidOAuthRequest) {
			writeAPIError(c, http.StatusBadRequest, "invalid_oauth_request", "invalid or expired OAuth request", "")
			return
		}
		handler.internalError(c, "complete GitHub OAuth", err)
		return
	}
	c.Redirect(http.StatusFound, redirect)
}

func (handler *AuthHandler) Exchange(c *gin.Context) {
	var request struct {
		Code             string `json:"code"`
		ExchangeVerifier string `json:"exchange_verifier"`
	}
	if !decodeJSON(c, &request) || strings.TrimSpace(request.Code) == "" || strings.TrimSpace(request.ExchangeVerifier) == "" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "code and exchange_verifier are required", "")
		return
	}
	tokens, err := handler.service.Exchange(c.Request.Context(), request.Code, request.ExchangeVerifier)
	if err != nil {
		if errors.Is(err, services.ErrInvalidExchangeCode) {
			writeAPIError(c, http.StatusBadRequest, "invalid_exchange_code", "invalid or expired exchange code", "")
			return
		}
		handler.internalError(c, "exchange OAuth code", err)
		return
	}
	c.JSON(http.StatusOK, tokens)
}

func (handler *AuthHandler) Refresh(c *gin.Context) {
	var request struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeJSON(c, &request) || strings.TrimSpace(request.RefreshToken) == "" {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "refresh_token is required", "")
		return
	}
	tokens, err := handler.service.Refresh(c.Request.Context(), request.RefreshToken)
	if err != nil {
		if errors.Is(err, services.ErrInvalidSession) {
			writeAPIError(c, http.StatusUnauthorized, "invalid_session", "invalid or expired session", "")
			return
		}
		handler.internalError(c, "refresh session", err)
		return
	}
	c.JSON(http.StatusOK, tokens)
}

func (handler *AuthHandler) Logout(c *gin.Context) {
	token, _ := bearerToken(c.GetHeader("Authorization"))
	if err := handler.service.Logout(c.Request.Context(), token); err != nil && !errors.Is(err, services.ErrInvalidSession) {
		handler.internalError(c, "logout session", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *AuthHandler) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			writeAPIError(c, http.StatusUnauthorized, "invalid_session", "bearer token is required", "")
			c.Abort()
			return
		}
		principal, err := handler.service.Authenticate(c.Request.Context(), token)
		if err != nil {
			if !errors.Is(err, services.ErrInvalidSession) {
				handler.logger.Error("session authentication failed", "error", err)
			}
			writeAPIError(c, http.StatusUnauthorized, "invalid_session", "invalid or expired session", "")
			c.Abort()
			return
		}
		c.Set(principalContextKey, principal)
		c.Next()
	}
}

func PrincipalFromContext(c *gin.Context) repository.Principal {
	value, _ := c.Get(principalContextKey)
	principal, _ := value.(repository.Principal)
	return principal
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	returnToken := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != ""
	if !returnToken {
		return "", false
	}
	return parts[1], true
}

func decodeJSON(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func writeAPIError(c *gin.Context, status int, code, message, feature string) {
	errorBody := gin.H{"code": code, "message": message}
	if feature != "" {
		errorBody["feature"] = feature
	}
	c.JSON(status, gin.H{"error": errorBody})
}

func (handler *AuthHandler) internalError(c *gin.Context, operation string, err error) {
	handler.logger.Error(operation+" failed", "error", err)
	writeAPIError(c, http.StatusInternalServerError, "internal_error", "internal server error", "")
}
