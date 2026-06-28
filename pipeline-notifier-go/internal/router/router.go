package router

import (
	"pipeline-notifier/internal/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", handlers.HealthCheckHandler)
	router.POST("/webhook/github", handlers.GithubWebhookHandler)
	router.GET("/pipelines/:id", handlers.GetPipelineStateHandler)

	return router
}
