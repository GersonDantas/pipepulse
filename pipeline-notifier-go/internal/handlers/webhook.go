package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type WorkflowRunService interface {
	Handle(context.Context, string, models.GithubWebhookPayload) error
}

type Handler struct {
	service    WorkflowRunService
	repository repository.StateRepository
	logger     *slog.Logger
}

func New(service WorkflowRunService, stateRepository repository.StateRepository, logger *slog.Logger) *Handler {
	return &Handler{service: service, repository: stateRepository, logger: logger}
}

func (handler *Handler) GithubWebhook(c *gin.Context) {
	if c.GetHeader("X-Hub-Signature-256") == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing signature"})
		return
	}

	deliveryID := c.GetHeader("X-GitHub-Delivery")
	if deliveryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing delivery id"})
		return
	}

	var payload models.GithubWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if err := handler.service.Handle(c.Request.Context(), deliveryID, payload); err != nil {
		handler.logger.Error("webhook handling failed", "delivery_id", deliveryID, "error", err)

		if errors.Is(err, services.ErrInvalidTimestamp) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid timestamp"})
			return
		}
		if errors.Is(err, services.ErrInvalidStatus) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "invalid workflow status",
				"allowed": []string{"running", "success", "cancelled", "failed"},
			})
			return
		}
		if errors.Is(err, services.ErrQueueUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "event queue unavailable"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
		return
	}

	c.Status(http.StatusAccepted)
}

func (handler *Handler) GetPipelineState(c *gin.Context) {
	state := handler.repository.GetState(c.Param("id"))
	if state == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "state not found"})
		return
	}
	c.JSON(http.StatusOK, state)
}

func (handler *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
