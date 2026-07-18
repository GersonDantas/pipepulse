package queue

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"pipeline-notifier/internal/models"
)

type recordingProcessor struct {
	events chan models.Event
}

func (processor *recordingProcessor) Process(_ context.Context, event models.Event) {
	processor.events <- event
}

func TestShutdownDrainsAcceptedEventsAndRejectsNewOnes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	queue := New(1, logger)
	processor := &recordingProcessor{events: make(chan models.Event, 1)}
	queue.Start(processor)

	event := models.Event{DeliveryID: "delivery-1"}
	if err := queue.Enqueue(context.Background(), event); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := queue.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case processed := <-processor.events:
		if processed.DeliveryID != "delivery-1" {
			t.Fatalf("DeliveryID = %q, want delivery-1", processed.DeliveryID)
		}
	default:
		t.Fatal("expected accepted event to be processed before shutdown")
	}

	if err := queue.Enqueue(context.Background(), models.Event{DeliveryID: "delivery-2"}); err != ErrQueueClosed {
		t.Fatalf("Enqueue() error = %v, want ErrQueueClosed", err)
	}
}
