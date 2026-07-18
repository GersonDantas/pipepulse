package router

import (
	"pipeline-notifier/internal/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRouter(handler *handlers.Handler) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", handler.HealthCheck)
	router.POST("/webhook/github", handler.GithubWebhook)
	router.GET("/pipelines/:id", handler.GetPipelineState)

	return router
}
