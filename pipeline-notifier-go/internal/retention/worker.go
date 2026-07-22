package retention

import (
	"context"
	"log/slog"
	"time"

	"pipeline-notifier/internal/repository"
)

type Store interface {
	Cleanup(context.Context, time.Time) (repository.RetentionResult, error)
}

type Worker struct {
	store    Store
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

func New(store Store, interval time.Duration, logger *slog.Logger) *Worker {
	return &Worker{store: store, interval: interval, logger: logger, now: time.Now}
}

func (worker *Worker) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(worker.interval)
		defer ticker.Stop()

		worker.cleanup(ctx)
		for {
			select {
			case <-ctx.Done():
				worker.logger.Info("retention worker stopped")
				return
			case <-ticker.C:
				worker.cleanup(ctx)
			}
		}
	}()
	return done
}

func (worker *Worker) cleanup(ctx context.Context) {
	result, err := worker.store.Cleanup(ctx, worker.now().UTC())
	if err != nil {
		worker.logger.Error("retention cleanup failed", "error", err)
		return
	}
	worker.logger.Info(
		"retention cleanup completed",
		"webhook_deliveries", result.WebhookDeliveries,
		"pipeline_failures", result.PipelineFailures,
		"notification_deliveries", result.NotificationDeliveries,
	)
}
