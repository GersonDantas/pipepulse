package router

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"pipeline-notifier/internal/handlers"

	"github.com/gin-gonic/gin"
)

func SetupRouter(handler *handlers.Handler, logger *slog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(structuredLogger(logger), gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		logger.Error("panic recovered", "error", recovered)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))

	router.GET("/health", handler.HealthCheck)
	router.POST("/webhook/github", handler.GithubWebhook)
	router.GET("/pipelines/:id", handler.GetPipelineState)

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
