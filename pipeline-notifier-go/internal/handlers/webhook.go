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

		c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
		return
	}

	c.Status(http.StatusOK)
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
