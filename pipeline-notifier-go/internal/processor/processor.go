package processor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
)

type Processor struct {
	repository repository.ProcessingRepository
	logger     *slog.Logger
}

func New(processingRepository repository.ProcessingRepository, logger *slog.Logger) *Processor {
	return &Processor{repository: processingRepository, logger: logger}
}

func (processor *Processor) Process(ctx context.Context, event models.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	transaction, err := processor.repository.BeginProcessing(ctx, event)
	if errors.Is(err, repository.ErrDeliveryAlreadyProcessed) {
		processor.logger.Debug("duplicate delivery ignored", "delivery_id", event.DeliveryID, "pipeline_id", event.PipelineKey())
		return nil
	}
	if err != nil {
		return fmt.Errorf("begin processing delivery %s: %w", event.DeliveryID, err)
	}
	defer transaction.Rollback(context.Background())

	current := transaction.CurrentState()
	if current != nil && event.Timestamp.Before(current.Timestamp) {
		return processor.ignore(ctx, transaction, event, "older_event")
	}
	if current != nil && event.Timestamp.Equal(current.Timestamp) {
		if event.Status.Priority() < current.Status.Priority() {
			return processor.ignore(ctx, transaction, event, "lower_priority")
		}
		if event.Status.Priority() == current.Status.Priority() &&
			event.WorkflowRunID == current.WorkflowRunID && event.RunAttempt == current.RunAttempt {
			return processor.ignore(ctx, transaction, event, "duplicate_run_state")
		}
	}

	state := repository.State{
		PipelineID:     event.PipelineKey(),
		RepositoryID:   event.RepositoryID,
		WorkflowID:     event.WorkflowID,
		WorkflowRunID:  event.WorkflowRunID,
		RunAttempt:     event.RunAttempt,
		Status:         event.Status,
		Conclusion:     event.Conclusion,
		Timestamp:      event.Timestamp.UTC(),
		LastDeliveryID: event.DeliveryID,
	}
	if err := transaction.SaveState(ctx, state); err != nil {
		return fmt.Errorf("save pipeline state: %w", err)
	}

	failureCreated := false
	notificationCount := int64(0)
	if event.Status == models.PipelineStatusFailed {
		failureID, created, err := transaction.CreateFailure(ctx, event)
		if err != nil {
			return fmt.Errorf("create pipeline failure: %w", err)
		}
		failureCreated = created
		if created {
			notificationCount, err = transaction.CreateNotifications(ctx, failureID)
			if err != nil {
				return fmt.Errorf("create notification outbox: %w", err)
			}
		}
	}

	if err := transaction.CompleteDelivery(ctx, ""); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return err
	}

	processor.logger.Info(
		"pipeline event processed",
		"delivery_id", event.DeliveryID,
		"pipeline_id", event.PipelineKey(),
		"status", event.Status,
		"failure_created", failureCreated,
		"notifications_created", notificationCount,
	)
	return nil
}

func (processor *Processor) ignore(ctx context.Context, transaction repository.EventTransaction, event models.Event, reason string) error {
	if err := transaction.CompleteDelivery(ctx, reason); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return err
	}
	processor.logger.Debug(
		"pipeline event ignored",
		"delivery_id", event.DeliveryID,
		"pipeline_id", event.PipelineKey(),
		"reason", reason,
	)
	return nil
}
