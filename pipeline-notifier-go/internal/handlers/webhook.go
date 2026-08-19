package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

type WorkflowRunService interface {
	Handle(context.Context, services.WebhookRequest) error
}

const maxWebhookBodyBytes = 1 << 20

type Handler struct {
	service    WorkflowRunService
	repository repository.StateReader
	logger     *slog.Logger
}

func New(service WorkflowRunService, stateRepository repository.StateReader, logger *slog.Logger) *Handler {
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
	if c.GetHeader("X-GitHub-Event") != "workflow_run" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported GitHub event"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBodyBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	err = handler.service.Handle(c.Request.Context(), services.WebhookRequest{
		EndpointID: c.Param("endpoint_id"),
		DeliveryID: deliveryID,
		Signature:  c.GetHeader("X-Hub-Signature-256"),
		Body:       body,
	})
	if err != nil {
		handler.logger.Error("webhook handling failed", "delivery_id", deliveryID, "error", err)

		switch {
		case errors.Is(err, services.ErrInvalidSignature):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		case errors.Is(err, services.ErrWebhookEndpointNotFound), errors.Is(err, services.ErrMonitoredWorkflowNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "webhook endpoint not found"})
		case errors.Is(err, services.ErrInvalidPayload), errors.Is(err, services.ErrInvalidTimestamp):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		case errors.Is(err, services.ErrInvalidStatus), errors.Is(err, services.ErrRepositoryMismatch):
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": "invalid workflow run",
			})
		case errors.Is(err, services.ErrQueueUnavailable):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "event queue unavailable"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
		}
		return
	}

	c.Status(http.StatusAccepted)
}

func (handler *Handler) GetPipelineState(c *gin.Context) {
	state, err := handler.repository.GetState(c.Request.Context(), c.Param("id"))
	if err != nil {
		handler.logger.Error("pipeline state lookup failed", "pipeline_id", c.Param("id"), "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
		return
	}
	if state == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "state not found"})
		return
	}
	c.JSON(http.StatusOK, state)
}

func (handler *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
