package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/services"
)

func GithubWebhookHandler(c *gin.Context) {
	var payload models.GithubWebhookPayload

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	if err := services.HandleWebhook(payload); err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error"})
		return
	}

	c.Status(http.StatusOK)
}
