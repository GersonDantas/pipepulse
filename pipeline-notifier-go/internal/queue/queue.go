package queue

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"pipeline-notifier/internal/models"
)

var ErrQueueClosed = errors.New("event queue is closed")
var ErrQueueFull = errors.New("event queue is full")

type EventProcessor interface {
	Process(context.Context, models.Event)
}

type EventQueue struct {
	events       chan models.Event
	logger       *slog.Logger
	mu           sync.RWMutex
	closed       bool
	shutdownOnce sync.Once
	done         chan struct{}
}

func New(size int, logger *slog.Logger) *EventQueue {
	return &EventQueue{
		events: make(chan models.Event, size),
		logger: logger,
		done:   make(chan struct{}),
	}
}

func (queue *EventQueue) Start(processor EventProcessor) {
	go func() {
		defer close(queue.done)
		for event := range queue.events {
			processor.Process(context.Background(), event)
		}
		queue.logger.Info("event worker stopped")
	}()
}

func (queue *EventQueue) Enqueue(ctx context.Context, event models.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	queue.mu.RLock()
	defer queue.mu.RUnlock()
	if queue.closed {
		return ErrQueueClosed
	}

	select {
	case queue.events <- event:
		queue.logger.Debug("event enqueued", "delivery_id", event.DeliveryID)
		return nil
	default:
		return ErrQueueFull
	}
}

func (queue *EventQueue) Shutdown(ctx context.Context) error {
	queue.shutdownOnce.Do(func() {
		queue.mu.Lock()
		queue.closed = true
		close(queue.events)
		queue.mu.Unlock()
	})

	select {
	case <-queue.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
