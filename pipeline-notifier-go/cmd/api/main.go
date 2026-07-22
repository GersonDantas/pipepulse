package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"pipeline-notifier/internal/config"
	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/handlers"
	"pipeline-notifier/internal/processor"
	"pipeline-notifier/internal/queue"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/retention"
	internalRouter "pipeline-notifier/internal/router"
	"pipeline-notifier/internal/services"
)

func main() {
	configuration, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: configuration.LogLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtimeContext, stopRuntime := context.WithCancel(ctx)
	defer stopRuntime()

	if err := database.Migrate(ctx, configuration.DatabaseURL); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	pool, err := database.Open(ctx, configuration.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := repository.NewPostgresStore(pool)
	eventProcessor := processor.New(store, logger)
	eventQueue := queue.New(store, configuration.WorkerPollInterval, logger)
	eventQueue.Start(eventProcessor)
	retentionDone := retention.New(store, configuration.RetentionInterval, logger).Start(runtimeContext)

	webhookService := services.NewWebhookService(eventQueue)
	handler := handlers.New(webhookService, store, logger)
	server := &http.Server{
		Addr:              ":" + configuration.Port,
		Handler:           internalRouter.SetupRouter(handler, logger),
		ReadHeaderTimeout: configuration.ReadHeaderTimeout,
		WriteTimeout:      configuration.WriteTimeout,
		IdleTimeout:       configuration.IdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server started", "port", configuration.Port, "environment", configuration.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownRequested := false
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownRequested = true
		logger.Info("shutdown requested")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), configuration.ShutdownTimeout)
	defer cancel()
	stopRuntime()
	if shutdownRequested {
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("graceful HTTP shutdown failed", "error", err)
			os.Exit(1)
		}
	}
	if err := eventQueue.Shutdown(shutdownContext); err != nil {
		logger.Error("event worker shutdown failed", "error", err)
		os.Exit(1)
	}
	select {
	case <-retentionDone:
	case <-shutdownContext.Done():
		logger.Error("retention worker shutdown failed", "error", shutdownContext.Err())
		os.Exit(1)
	}
	if !shutdownRequested {
		if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server close failed", "error", err)
			os.Exit(1)
		}
	}
	logger.Info("server stopped")
}
