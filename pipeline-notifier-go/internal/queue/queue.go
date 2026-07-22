package queue

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"pipeline-notifier/internal/models"
)

var ErrQueueClosed = errors.New("event queue is closed")

type DeliveryStore interface {
	Enqueue(context.Context, models.Event) (bool, error)
	NextPending(context.Context) (*models.Event, error)
}

type EventProcessor interface {
	Process(context.Context, models.Event) error
}

type EventQueue struct {
	store        DeliveryStore
	wakeups      chan struct{}
	pollInterval time.Duration
	logger       *slog.Logger

	mu           sync.RWMutex
	closed       bool
	started      bool
	cancel       context.CancelFunc
	done         chan struct{}
	shutdownOnce sync.Once
}

func New(store DeliveryStore, pollInterval time.Duration, logger *slog.Logger) *EventQueue {
	return &EventQueue{
		store:        store,
		wakeups:      make(chan struct{}, 1),
		pollInterval: pollInterval,
		logger:       logger,
		done:         make(chan struct{}),
	}
}

func (queue *EventQueue) Start(processor EventProcessor) {
	queue.mu.Lock()
	if queue.started || queue.closed {
		queue.mu.Unlock()
		return
	}
	queue.started = true
	workerContext, cancel := context.WithCancel(context.Background())
	queue.cancel = cancel
	queue.mu.Unlock()

	go func() {
		defer close(queue.done)
		ticker := time.NewTicker(queue.pollInterval)
		defer ticker.Stop()

		queue.drain(workerContext, processor)
		for {
			select {
			case <-workerContext.Done():
				queue.logger.Info("event worker stopped")
				return
			case <-queue.wakeups:
				queue.drain(workerContext, processor)
			case <-ticker.C:
				queue.drain(workerContext, processor)
			}
		}
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

	inserted, err := queue.store.Enqueue(ctx, event)
	if err != nil {
		return err
	}
	if !inserted {
		queue.logger.Debug("duplicate delivery already persisted", "delivery_id", event.DeliveryID)
		return nil
	}

	select {
	case queue.wakeups <- struct{}{}:
	default:
	}
	queue.logger.Debug("delivery persisted and worker signalled", "delivery_id", event.DeliveryID)
	return nil
}

func (queue *EventQueue) Shutdown(ctx context.Context) error {
	queue.shutdownOnce.Do(func() {
		queue.mu.Lock()
		queue.closed = true
		if queue.cancel != nil {
			queue.cancel()
		} else {
			close(queue.done)
		}
		queue.mu.Unlock()
	})

	select {
	case <-queue.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (queue *EventQueue) drain(ctx context.Context, processor EventProcessor) {
	for ctx.Err() == nil {
		event, err := queue.store.NextPending(ctx)
		if err != nil {
			queue.logger.Error("pending delivery lookup failed", "error", err)
			return
		}
		if event == nil {
			return
		}
		if err := processor.Process(ctx, *event); err != nil {
			queue.logger.Error("delivery processing failed", "delivery_id", event.DeliveryID, "error", err)
			return
		}
	}
}
