package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type ProductApplication interface {
	Bootstrap(repository.Principal) (services.Bootstrap, error)
	CreateRepository(context.Context, repository.Principal, repository.CreateRepositoryInput) (repository.ProductRepository, error)
	ListRepositories(context.Context, repository.Principal) ([]repository.ProductRepository, error)
	GetRepository(context.Context, repository.Principal, string) (repository.ProductRepository, error)
	RotateRepositorySecret(context.Context, repository.Principal, string) (services.WebhookCredentials, error)
	DeleteRepository(context.Context, repository.Principal, string) error
	ListFailures(context.Context, repository.Principal, string, int) (services.FailurePage, error)
	PutDevice(context.Context, repository.Principal, string, string) error
	DeleteDevice(context.Context, repository.Principal) error
	DeleteAccount(context.Context, repository.Principal) error
}

type ProductHandler struct {
	service ProductApplication
	logger  *slog.Logger
}

func NewProduct(service ProductApplication, logger *slog.Logger) *ProductHandler {
	return &ProductHandler{service: service, logger: logger}
}

func (handler *ProductHandler) Bootstrap(c *gin.Context) {
	result, err := handler.service.Bootstrap(PrincipalFromContext(c))
	if err != nil {
		handler.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (handler *ProductHandler) CreateRepository(c *gin.Context) {
	var input repository.CreateRepositoryInput
	if !decodeJSON(c, &input) {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "invalid repository payload", "")
		return
	}
	result, err := handler.service.CreateRepository(c.Request.Context(), PrincipalFromContext(c), input)
	if err != nil {
		handler.handleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (handler *ProductHandler) ListRepositories(c *gin.Context) {
	result, err := handler.service.ListRepositories(c.Request.Context(), PrincipalFromContext(c))
	if err != nil {
		handler.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"repositories": result})
}

func (handler *ProductHandler) GetRepository(c *gin.Context) {
	result, err := handler.service.GetRepository(c.Request.Context(), PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		handler.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (handler *ProductHandler) RotateRepositorySecret(c *gin.Context) {
	result, err := handler.service.RotateRepositorySecret(c.Request.Context(), PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		handler.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (handler *ProductHandler) DeleteRepository(c *gin.Context) {
	if err := handler.service.DeleteRepository(c.Request.Context(), PrincipalFromContext(c), c.Param("id")); err != nil {
		handler.handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *ProductHandler) ListFailures(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeAPIError(c, http.StatusBadRequest, "invalid_request", "limit must be a positive integer", "")
			return
		}
		limit = parsed
	}
	result, err := handler.service.ListFailures(c.Request.Context(), PrincipalFromContext(c), c.Query("cursor"), limit)
	if err != nil {
		handler.handleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (handler *ProductHandler) PutDevice(c *gin.Context) {
	var input struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if !decodeJSON(c, &input) {
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "invalid device payload", "")
		return
	}
	if err := handler.service.PutDevice(c.Request.Context(), PrincipalFromContext(c), input.Token, input.Platform); err != nil {
		handler.handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *ProductHandler) DeleteDevice(c *gin.Context) {
	if err := handler.service.DeleteDevice(c.Request.Context(), PrincipalFromContext(c)); err != nil {
		handler.handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *ProductHandler) DeleteAccount(c *gin.Context) {
	if err := handler.service.DeleteAccount(c.Request.Context(), PrincipalFromContext(c)); err != nil {
		handler.handleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (handler *ProductHandler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidProductRequest), errors.Is(err, services.ErrInvalidCursor):
		writeAPIError(c, http.StatusBadRequest, "invalid_request", "invalid request", "")
	case errors.Is(err, repository.ErrProductNotFound):
		writeAPIError(c, http.StatusNotFound, "not_found", "resource not found", "")
	case errors.Is(err, repository.ErrRepositoryLimit):
		writeAPIError(c, http.StatusForbidden, "entitlement_required", "repository limit reached", "max_repositories")
	case errors.Is(err, repository.ErrRepositoryExists):
		writeAPIError(c, http.StatusConflict, "repository_exists", "repository already exists", "")
	case errors.Is(err, repository.ErrDeviceTokenInUse):
		writeAPIError(c, http.StatusConflict, "device_token_in_use", "device token is already registered", "")
	default:
		handler.logger.Error("product request failed", "error", err)
		writeAPIError(c, http.StatusInternalServerError, "internal_error", "internal server error", "")
	}
}
