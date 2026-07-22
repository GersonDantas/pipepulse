package processor

import (
	"context"
	"log/slog"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
)

type Notifier interface {
	Notify(context.Context, models.Event) error
}

type NotifierFunc func(context.Context, models.Event) error

func (function NotifierFunc) Notify(ctx context.Context, event models.Event) error {
	return function(ctx, event)
}

type Processor struct {
	repository repository.StateRepository
	notifier   Notifier
	logger     *slog.Logger
}

func New(stateRepository repository.StateRepository, notifier Notifier, logger *slog.Logger) *Processor {
	return &Processor{
		repository: stateRepository,
		notifier:   notifier,
		logger:     logger,
	}
}

func (processor *Processor) Process(ctx context.Context, event models.Event) {
	if err := ctx.Err(); err != nil {
		processor.logger.Debug("event skipped because context is cancelled", "delivery_id", event.DeliveryID)
		return
	}

	pipelineID := event.PipelineKey()
	current := processor.repository.GetState(pipelineID)

	if current != nil && current.LastDeliveryID == event.DeliveryID {
		processor.logger.Debug("duplicate delivery ignored", "delivery_id", event.DeliveryID, "pipeline_id", pipelineID)
		return
	}

	if current != nil && event.Timestamp.Before(current.Timestamp) {
		processor.logger.Debug("older event ignored", "delivery_id", event.DeliveryID, "pipeline_id", pipelineID)
		return
	}

	if current != nil && event.Timestamp.Equal(current.Timestamp) {
		if event.Status.Priority() < current.Status.Priority() ||
			(event.Status.Priority() == current.Status.Priority() &&
				event.WorkflowRunID == current.WorkflowRunID && event.RunAttempt == current.RunAttempt) {
			processor.logger.Debug("equal timestamp event with lower priority or same run ignored", "delivery_id", event.DeliveryID, "pipeline_id", pipelineID)
			return
		}
	}

	processor.repository.SaveState(repository.State{
		PipelineID:     pipelineID,
		RepositoryID:   event.RepositoryID,
		WorkflowID:     event.WorkflowID,
		WorkflowRunID:  event.WorkflowRunID,
		RunAttempt:     event.RunAttempt,
		Status:         event.Status,
		Conclusion:     event.Conclusion,
		Timestamp:      event.Timestamp,
		LastDeliveryID: event.DeliveryID,
	})

	processor.logger.Info("pipeline state updated", "delivery_id", event.DeliveryID, "pipeline_id", pipelineID, "status", event.Status)

	if processor.shouldNotify(current, event) {
		if err := processor.notifier.Notify(ctx, event); err != nil {
			processor.logger.Error("notification failed", "delivery_id", event.DeliveryID, "error", err)
		}
	}
}

func (processor *Processor) shouldNotify(current *repository.State, event models.Event) bool {
	if event.Status != models.PipelineStatusFailed {
		return false
	}
	if current == nil {
		return true
	}
	return current.WorkflowRunID != event.WorkflowRunID || current.RunAttempt != event.RunAttempt
}

func NewLogNotifier(logger *slog.Logger) Notifier {
	return NotifierFunc(func(_ context.Context, event models.Event) error {
		logger.Info("notification queued for future delivery", "delivery_id", event.DeliveryID, "workflow_run_id", event.WorkflowRunID, "status", event.Status)
		return nil
	})
}
