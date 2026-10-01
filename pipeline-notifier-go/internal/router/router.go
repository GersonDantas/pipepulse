package router

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"pipeline-notifier/internal/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRouter(handler *handlers.Handler, authHandler *handlers.AuthHandler, productHandler *handlers.ProductHandler, logger *slog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(structuredLogger(logger), gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		logger.Error("panic recovered", "error", recovered)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))

	router.GET("/health", handler.HealthCheck)
	router.GET("/health/live", handler.HealthCheck)
	router.GET("/health/ready", handler.HealthCheck)
	router.POST("/webhooks/github/:endpoint_id", handler.GithubWebhook)

	if authHandler != nil {
		router.POST("/v1/auth/github/start", authHandler.GithubStart)
		router.GET("/v1/auth/github/callback", authHandler.GithubCallback)
		router.POST("/v1/auth/github/exchange", authHandler.Exchange)
		router.POST("/v1/auth/refresh", authHandler.Refresh)
	}
	if authHandler != nil && productHandler != nil {
		api := router.Group("/v1", authHandler.RequireSession())
		api.POST("/auth/logout", authHandler.Logout)
		api.GET("/bootstrap", productHandler.Bootstrap)
		api.POST("/repositories", productHandler.CreateRepository)
		api.GET("/repositories", productHandler.ListRepositories)
		api.GET("/repositories/:id", productHandler.GetRepository)
		api.POST("/repositories/:id/rotate-webhook-secret", productHandler.RotateRepositorySecret)
		api.DELETE("/repositories/:id", productHandler.DeleteRepository)
		api.GET("/failures", productHandler.ListFailures)
		api.PUT("/device", productHandler.PutDevice)
		api.DELETE("/device", productHandler.DeleteDevice)
		api.DELETE("/account", productHandler.DeleteAccount)
	}

	return router
}

func structuredLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		logger.Info("http request",
			"status", c.Writer.Status(),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"latency", time.Since(startedAt),
			"client_ip", c.ClientIP(),
		)
	}
}
