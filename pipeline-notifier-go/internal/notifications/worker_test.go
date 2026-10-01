package notifications

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

type fakeStore struct {
	delivery *repository.Notification
	results  []repository.NotificationResult
	inactive bool
}

func (s *fakeStore) ClaimNotification(context.Context, time.Time) (*repository.Notification, error) {
	d := s.delivery
	s.delivery = nil
	return d, nil
}
func (s *fakeStore) FinishNotification(_ context.Context, _ repository.Notification, result repository.NotificationResult) error {
	s.results = append(s.results, result)
	return nil
}

type fakeSender struct {
	messages []Message
	err      error
}

func (s *fakeStore) NotificationActive(context.Context, repository.Notification) (bool, error) {
	return !s.inactive, nil
}

type senderFunc func(context.Context, Message) error

func (f senderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func TestWorkerRejectsInactiveAndUndecryptableRecipients(t *testing.T) {
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	for _, inactive := range []bool{true, false} {
		store := &fakeStore{delivery: &repository.Notification{ID: "n", Attempt: 1, TokenCiphertext: []byte("bad")}, inactive: inactive}
		sender := &fakeSender{}
		worker := NewWorker(store, sender, box, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
		worker.drain(context.Background())
		if len(sender.messages) != 0 || len(store.results) != 1 || store.results[0].Status != "abandoned" || store.results[0].DisableDevice {
			t.Fatalf("unexpected messages/results")
		}
	}
}

func TestWorkerHonorsRetryAfterAndSanitizesLogs(t *testing.T) {
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	ciphertext, _ := box.Encrypt([]byte("secret-device-token"))
	store := &fakeStore{delivery: &repository.Notification{ID: "n", Attempt: 1, TokenCiphertext: ciphertext}}
	var logs bytes.Buffer
	worker := NewWorker(store, &fakeSender{err: &SendError{Code: "fcm_unavailable", RetryAfter: 15 * time.Minute}}, box, time.Second, slog.New(slog.NewJSONHandler(&logs, nil)))
	now := time.Now().UTC()
	worker.now = func() time.Time { return now }
	worker.drain(context.Background())
	if !store.results[0].NextAttemptAt.Equal(now.Add(15*time.Minute)) || bytes.Contains(logs.Bytes(), []byte("secret-device-token")) {
		t.Fatalf("retry/log validation failed")
	}
}

func TestWorkerStartupRecoveryAndShutdownCancellation(t *testing.T) {
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	ciphertext, _ := box.Encrypt([]byte("token"))
	store := &fakeStore{delivery: &repository.Notification{ID: "n", Attempt: 1, TokenCiphertext: ciphertext}}
	started := make(chan struct{})
	sender := senderFunc(func(ctx context.Context, _ Message) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("attempt deadline missing")
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	worker := NewWorker(store, sender, box, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := worker.Start(ctx)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("startup did not recover pending delivery")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker failed to stop")
	}
	if len(store.results) != 0 {
		t.Fatal("shutdown did not preserve claim for recovery")
	}
}
func (s *fakeSender) Send(_ context.Context, message Message) error {
	s.messages = append(s.messages, message)
	return s.err
}

func TestWorkerSendsDecryptedTokenAndCompletesOutbox(t *testing.T) {
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	ciphertext, _ := box.Encrypt([]byte("private-device-token"))
	store := &fakeStore{delivery: &repository.Notification{ID: "notification-1", FailureID: "failure-1", RepositoryID: "repository-1", TokenCiphertext: ciphertext, Attempt: 1}}
	sender := &fakeSender{}
	worker := NewWorker(store, sender, box, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.drain(context.Background())
	if len(sender.messages) != 1 || sender.messages[0].Token != "private-device-token" || sender.messages[0].NotificationID != "notification-1" {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if len(store.results) != 1 || store.results[0].Status != "sent" {
		t.Fatalf("results = %#v", store.results)
	}
}

func TestWorkerSchedulesRetriesAndAbandonsAfterFiveAttempts(t *testing.T) {
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	ciphertext, _ := box.Encrypt([]byte("token"))
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 0}
	for attempt, delay := range delays {
		store := &fakeStore{delivery: &repository.Notification{ID: "n", TokenCiphertext: ciphertext, Attempt: attempt + 1}}
		worker := NewWorker(store, &fakeSender{err: errors.New("sensitive remote error")}, box, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
		worker.now = func() time.Time { return now }
		worker.drain(context.Background())
		result := store.results[0]
		status := "failed"
		if attempt == 4 {
			status = "abandoned"
		}
		if result.Status != status || (delay > 0 && !result.NextAttemptAt.Equal(now.Add(delay))) || result.ErrorCode != "transport_error" {
			t.Fatalf("attempt %d result = %#v", attempt+1, result)
		}
	}
}
