package handlers

import (
	"errors"
	"log"
	"net/http"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/services"

	"github.com/gin-gonic/gin"
)

func GithubWebhookHandler(c *gin.Context) {
	var payload models.GithubWebhookPayload
	signature := c.GetHeader("X-Hub-Signature-256")
	if signature == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing signature"})
		return
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if err := services.HandleWebhook(payload); err != nil {
		log.Println(err)

		if errors.Is(err, services.ErrInvalidTimestamp) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid timestamp"})
			return
		}

		if errors.Is(err, services.ErrInvalidStatus) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "invalid status",
				"allowed": []string{"failed", "success", "running"},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
		return
	}

	c.Status(http.StatusAccepted)
}

func GetPipelineStateHandler(c *gin.Context) {
	id := c.Param("id")

	state := repository.GetState(id)
	if state == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "state not found"})
		return
	}

	c.JSON(http.StatusOK, state)
}

func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
