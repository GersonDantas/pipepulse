package processor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
)

type fakeNotifier struct {
	events []models.Event
}

func (notifier *fakeNotifier) Notify(_ context.Context, event models.Event) error {
	notifier.events = append(notifier.events, event)
	return nil
}

func newTestProcessor() (*Processor, *repository.MemoryStateRepository, *fakeNotifier) {
	stateRepository := repository.NewMemoryStateRepository()
	notifier := &fakeNotifier{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(stateRepository, notifier, logger), stateRepository, notifier
}

func workflowEvent(deliveryID string, status models.PipelineStatus, timestamp time.Time) models.Event {
	return models.Event{
		DeliveryID:    deliveryID,
		RepositoryID:  10,
		WorkflowID:    20,
		WorkflowRunID: 30,
		RunAttempt:    1,
		Status:        status,
		Timestamp:     timestamp,
	}
}

func TestProcessSavesPipelineStateWithoutNotifyingRunningEvent(t *testing.T) {
	processor, stateRepository, notifier := newTestProcessor()
	event := workflowEvent("delivery-1", models.PipelineStatusRunning, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))

	processor.Process(context.Background(), event)

	state := stateRepository.GetState(event.PipelineKey())
	if state == nil {
		t.Fatal("expected state to be saved")
	}
	if state.LastDeliveryID != "delivery-1" {
		t.Fatalf("LastDeliveryID = %q, want delivery-1", state.LastDeliveryID)
	}
	if len(notifier.events) != 0 {
		t.Fatalf("notifications = %d, want 0", len(notifier.events))
	}
}

func TestProcessNotifiesEachNewFailedRunOnce(t *testing.T) {
	processor, _, notifier := newTestProcessor()
	timestamp := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	failed := workflowEvent("delivery-1", models.PipelineStatusFailed, timestamp)
	processor.Process(context.Background(), failed)
	processor.Process(context.Background(), failed)

	nextFailedRun := workflowEvent("delivery-2", models.PipelineStatusFailed, timestamp.Add(time.Minute))
	nextFailedRun.WorkflowRunID = 31
	processor.Process(context.Background(), nextFailedRun)

	if len(notifier.events) != 2 {
		t.Fatalf("notifications = %d, want 2", len(notifier.events))
	}
}

func TestProcessIgnoresOlderAndLowerPriorityEvents(t *testing.T) {
	processor, stateRepository, notifier := newTestProcessor()
	timestamp := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	processor.Process(context.Background(), workflowEvent("delivery-1", models.PipelineStatusFailed, timestamp))
	processor.Process(context.Background(), workflowEvent("delivery-2", models.PipelineStatusRunning, timestamp.Add(-time.Minute)))
	processor.Process(context.Background(), workflowEvent("delivery-3", models.PipelineStatusSuccess, timestamp))

	state := stateRepository.GetState("10:20")
	if state == nil {
		t.Fatal("expected state to exist")
	}
	if state.Status != models.PipelineStatusFailed {
		t.Fatalf("Status = %q, want failed", state.Status)
	}
	if state.LastDeliveryID != "delivery-1" {
		t.Fatalf("LastDeliveryID = %q, want delivery-1", state.LastDeliveryID)
	}
	if len(notifier.events) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notifier.events))
	}
}
