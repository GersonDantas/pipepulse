package retention

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"pipeline-notifier/internal/repository"
)

func TestWorkerCleansOnStartupAndPeriodicallyUntilCancelled(t *testing.T) {
	store := &fakeStore{calls: make(chan time.Time, 3)}
	worker := New(store, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := worker.Start(ctx)

	for call := 1; call <= 2; call++ {
		select {
		case <-store.calls:
		case <-time.After(time.Second):
			t.Fatalf("cleanup call %d did not happen", call)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention worker did not stop after cancellation")
	}
}

type fakeStore struct {
	mu    sync.Mutex
	calls chan time.Time
}

func (store *fakeStore) Cleanup(_ context.Context, now time.Time) (repository.RetentionResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls <- now
	return repository.RetentionResult{WebhookDeliveries: 1}, nil
}
