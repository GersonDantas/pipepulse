package processor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
)

func TestProcessAppliesTemporalRulesAndCreatesFailureOutbox(t *testing.T) {
	t.Run("running event updates state without failure", func(t *testing.T) {
		processor, store := newTestProcessor()
		event := workflowEvent("delivery-1", models.PipelineStatusRunning, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))

		if err := processor.Process(context.Background(), event); err != nil {
			t.Fatalf("Process() error = %v", err)
		}

		state := store.states[event.PipelineKey()]
		if state.LastDeliveryID != event.DeliveryID {
			t.Fatalf("LastDeliveryID = %q, want %q", state.LastDeliveryID, event.DeliveryID)
		}
		if len(store.failures) != 0 || store.notifications != 0 {
			t.Fatalf("failures = %d, notifications = %d, want zero", len(store.failures), store.notifications)
		}
	})

	t.Run("running to failed in the same run creates exactly one failure", func(t *testing.T) {
		processor, store := newTestProcessor()
		timestamp := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		running := workflowEvent("delivery-1", models.PipelineStatusRunning, timestamp)
		failed := workflowEvent("delivery-2", models.PipelineStatusFailed, timestamp.Add(time.Minute))

		if err := processor.Process(context.Background(), running); err != nil {
			t.Fatalf("Process(running) error = %v", err)
		}
		if err := processor.Process(context.Background(), failed); err != nil {
			t.Fatalf("Process(failed) error = %v", err)
		}
		repeated := failed
		repeated.DeliveryID = "delivery-3"
		repeated.Timestamp = repeated.Timestamp.Add(time.Minute)
		if err := processor.Process(context.Background(), repeated); err != nil {
			t.Fatalf("Process(repeated) error = %v", err)
		}

		if len(store.failures) != 1 || store.notifications != 1 {
			t.Fatalf("failures = %d, notifications = %d, want 1 each", len(store.failures), store.notifications)
		}
	})

	t.Run("different failed runs with equal timestamp each create a failure", func(t *testing.T) {
		processor, store := newTestProcessor()
		timestamp := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		first := workflowEvent("delivery-1", models.PipelineStatusFailed, timestamp)
		second := workflowEvent("delivery-2", models.PipelineStatusFailed, timestamp)
		second.WorkflowRunID = 31

		if err := processor.Process(context.Background(), first); err != nil {
			t.Fatalf("Process(first) error = %v", err)
		}
		if err := processor.Process(context.Background(), second); err != nil {
			t.Fatalf("Process(second) error = %v", err)
		}

		if len(store.failures) != 2 || store.notifications != 2 {
			t.Fatalf("failures = %d, notifications = %d, want 2 each", len(store.failures), store.notifications)
		}
	})

	t.Run("older and lower priority events complete without changing state", func(t *testing.T) {
		processor, store := newTestProcessor()
		timestamp := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		failed := workflowEvent("delivery-1", models.PipelineStatusFailed, timestamp)
		older := workflowEvent("delivery-2", models.PipelineStatusRunning, timestamp.Add(-time.Minute))
		lowerPriority := workflowEvent("delivery-3", models.PipelineStatusSuccess, timestamp)

		for _, event := range []models.Event{failed, older, lowerPriority} {
			if err := processor.Process(context.Background(), event); err != nil {
				t.Fatalf("Process(%s) error = %v", event.DeliveryID, err)
			}
		}

		state := store.states[failed.PipelineKey()]
		if state.LastDeliveryID != failed.DeliveryID || state.Status != models.PipelineStatusFailed {
			t.Fatalf("state = %#v, want first failed event", state)
		}
		if store.completed[older.DeliveryID] != "older_event" || store.completed[lowerPriority.DeliveryID] != "lower_priority" {
			t.Fatalf("ignored reasons = %#v", store.completed)
		}
	})

	t.Run("transaction error rolls back state and delivery", func(t *testing.T) {
		processor, store := newTestProcessor()
		store.failureError = errors.New("database unavailable")
		event := workflowEvent("delivery-1", models.PipelineStatusFailed, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))

		if err := processor.Process(context.Background(), event); err == nil {
			t.Fatal("Process() error = nil, want failure")
		}
		if len(store.states) != 0 || len(store.completed) != 0 {
			t.Fatalf("state or delivery committed after error: states=%#v completed=%#v", store.states, store.completed)
		}
	})
}

func workflowEvent(deliveryID string, status models.PipelineStatus, timestamp time.Time) models.Event {
	return models.Event{
		DeliveryID:    deliveryID,
		RepositoryID:  10,
		WorkflowID:    20,
		WorkflowRunID: 30,
		RunAttempt:    1,
		Status:        status,
		Conclusion:    string(status),
		Timestamp:     timestamp,
	}
}

func newTestProcessor() (*Processor, *fakeRepository) {
	store := &fakeRepository{
		states:    make(map[string]repository.State),
		failures:  make(map[string]bool),
		completed: make(map[string]string),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(store, logger), store
}

type fakeRepository struct {
	states        map[string]repository.State
	failures      map[string]bool
	completed     map[string]string
	notifications int
	failureError  error
}

func (store *fakeRepository) BeginProcessing(_ context.Context, event models.Event) (repository.EventTransaction, error) {
	current, ok := store.states[event.PipelineKey()]
	var currentPointer *repository.State
	if ok {
		copy := current
		currentPointer = &copy
	}
	return &fakeTransaction{store: store, event: event, current: currentPointer}, nil
}

type fakeTransaction struct {
	store               *fakeRepository
	event               models.Event
	current             *repository.State
	state               *repository.State
	failureKey          string
	failureCreated      bool
	notifications       int
	ignoredReason       string
	deliveryWasComplete bool
	committed           bool
}

func (transaction *fakeTransaction) CurrentState() *repository.State {
	return transaction.current
}

func (transaction *fakeTransaction) SaveState(_ context.Context, state repository.State) error {
	transaction.state = &state
	return nil
}

func (transaction *fakeTransaction) CreateFailure(_ context.Context, event models.Event) (string, bool, error) {
	if transaction.store.failureError != nil {
		return "", false, transaction.store.failureError
	}
	key := fmt.Sprintf("%d:%d:%d:%d", event.RepositoryID, event.WorkflowID, event.WorkflowRunID, event.RunAttempt)
	if transaction.store.failures[key] {
		return "", false, nil
	}
	transaction.failureKey = key
	transaction.failureCreated = true
	return key, true, nil
}

func (transaction *fakeTransaction) CreateNotifications(_ context.Context, _ string) (int64, error) {
	transaction.notifications++
	return 1, nil
}

func (transaction *fakeTransaction) CompleteDelivery(_ context.Context, ignoredReason string) error {
	transaction.deliveryWasComplete = true
	transaction.ignoredReason = ignoredReason
	return nil
}

func (transaction *fakeTransaction) Commit(context.Context) error {
	if transaction.state != nil {
		transaction.store.states[transaction.event.PipelineKey()] = *transaction.state
	}
	if transaction.failureCreated {
		transaction.store.failures[transaction.failureKey] = true
	}
	transaction.store.notifications += transaction.notifications
	if transaction.deliveryWasComplete {
		transaction.store.completed[transaction.event.DeliveryID] = transaction.ignoredReason
	}
	transaction.committed = true
	return nil
}

func (transaction *fakeTransaction) Rollback(context.Context) error {
	return nil
}
