package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"pipeline-notifier/internal/models"
)

func TestDurableQueuePersistsBeforeSignallingAndRecoversPendingDeliveries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newFakeDeliveryStore()
	eventQueue := New(store, 10*time.Millisecond, logger)

	event := models.Event{DeliveryID: "delivery-1"}
	if err := eventQueue.Enqueue(context.Background(), event); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if !store.hasPending("delivery-1") {
		t.Fatal("delivery was signalled before being persisted")
	}

	processor := &recordingProcessor{events: make(chan models.Event, 2)}
	eventQueue.Start(processor)
	assertProcessed(t, processor.events, "delivery-1")

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := eventQueue.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := eventQueue.Enqueue(context.Background(), models.Event{DeliveryID: "delivery-2"}); !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("Enqueue() error = %v, want ErrQueueClosed", err)
	}
}

func TestDurableQueueDrainsPendingDeliveriesOnStartupWithoutSignal(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newFakeDeliveryStore()
	store.pending = append(store.pending,
		models.Event{DeliveryID: "delivery-before-restart-1"},
		models.Event{DeliveryID: "delivery-before-restart-2"},
	)
	eventQueue := New(store, time.Hour, logger)
	processor := &recordingProcessor{events: make(chan models.Event, 2)}

	eventQueue.Start(processor)
	assertProcessed(t, processor.events, "delivery-before-restart-1")
	assertProcessed(t, processor.events, "delivery-before-restart-2")

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := eventQueue.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func assertProcessed(t *testing.T, events <-chan models.Event, deliveryID string) {
	t.Helper()
	select {
	case event := <-events:
		if event.DeliveryID != deliveryID {
			t.Fatalf("DeliveryID = %q, want %q", event.DeliveryID, deliveryID)
		}
	case <-time.After(time.Second):
		t.Fatalf("delivery %q was not processed", deliveryID)
	}
}

type recordingProcessor struct {
	events chan models.Event
}

func (processor *recordingProcessor) Process(_ context.Context, event models.Event) error {
	processor.events <- event
	return nil
}

type fakeDeliveryStore struct {
	mu      sync.Mutex
	pending []models.Event
	known   map[string]bool
}

func newFakeDeliveryStore() *fakeDeliveryStore {
	return &fakeDeliveryStore{known: make(map[string]bool)}
}

func (store *fakeDeliveryStore) Enqueue(_ context.Context, event models.Event) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.known[event.DeliveryID] {
		return false, nil
	}
	store.known[event.DeliveryID] = true
	store.pending = append(store.pending, event)
	return true, nil
}

func (store *fakeDeliveryStore) NextPending(context.Context) (*models.Event, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.pending) == 0 {
		return nil, nil
	}
	event := store.pending[0]
	store.pending = store.pending[1:]
	return &event, nil
}

func (store *fakeDeliveryStore) hasPending(deliveryID string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, event := range store.pending {
		if event.DeliveryID == deliveryID {
			return true
		}
	}
	return false
}
