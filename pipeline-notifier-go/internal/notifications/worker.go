package notifications

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

type Message struct {
	Token          string
	NotificationID string
	FailureID      string
	RepositoryID   string
}

type Sender interface {
	Send(context.Context, Message) error
}
type Store interface {
	ClaimNotification(context.Context, time.Time) (*repository.Notification, error)
	NotificationActive(context.Context, repository.Notification) (bool, error)
	FinishNotification(context.Context, repository.Notification, repository.NotificationResult) error
}

// SendError contains only a fixed code, never the provider response or credentials.
type SendError struct {
	Code         string
	Permanent    bool
	InvalidToken bool
	RetryAfter   time.Duration
}

func (e *SendError) Error() string { return e.Code }

type Worker struct {
	store    Store
	sender   Sender
	box      *secrets.Box
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

func NewWorker(store Store, sender Sender, box *secrets.Box, interval time.Duration, logger *slog.Logger) *Worker {
	return &Worker{store: store, sender: sender, box: box, interval: interval, logger: logger, now: time.Now}
}
func (w *Worker) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		w.drain(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.drain(ctx)
			}
		}
	}()
	return done
}

func (w *Worker) drain(ctx context.Context) {
	for ctx.Err() == nil {
		// The deadline starts before claiming; the durable lease is at least one minute.
		attemptContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		delivery, err := w.store.ClaimNotification(attemptContext, w.now().UTC())
		if err != nil || delivery == nil {
			cancel()
			if err != nil {
				w.logger.Error("notification claim failed")
			}
			return
		}
		active, err := w.store.NotificationActive(attemptContext, *delivery)
		if err != nil {
			cancel()
			w.logger.Error("notification eligibility lookup failed")
			return
		}
		var sendErr error
		if !active {
			sendErr = &SendError{Code: "recipient_inactive", Permanent: true}
		} else {
			token, decryptErr := w.box.Decrypt(delivery.TokenCiphertext)
			if decryptErr != nil || len(token) == 0 {
				sendErr = &SendError{Code: "token_decryption_failed", Permanent: true}
			} else {
				sendErr = w.sender.Send(attemptContext, Message{Token: string(token), NotificationID: delivery.ID, FailureID: delivery.FailureID, RepositoryID: delivery.RepositoryID})
			}
		}
		cancel()
		// Cancellation leaves the claim durable for recovery, without a new request.
		if ctx.Err() != nil {
			return
		}
		result := w.result(*delivery, sendErr)
		finishContext, finishCancel := context.WithTimeout(ctx, 5*time.Second)
		err = w.store.FinishNotification(finishContext, *delivery, result)
		finishCancel()
		if err != nil {
			w.logger.Error("notification completion failed", "notification_id", delivery.ID)
			return
		}
		w.logger.Info("notification attempt completed", "notification_id", delivery.ID, "attempt", delivery.Attempt, "status", result.Status, "error_code", result.ErrorCode)
	}
}

func (w *Worker) result(delivery repository.Notification, err error) repository.NotificationResult {
	now := w.now().UTC()
	result := repository.NotificationResult{Status: "sent", CompletedAt: now}
	if err == nil {
		return result
	}
	result.Status = "failed"
	result.ErrorCode = "transport_error"
	var provider *SendError
	if errors.As(err, &provider) {
		result.ErrorCode = provider.Code
		result.DisableDevice = provider.InvalidToken
		if provider.Permanent {
			result.Status = "abandoned"
		}
	}
	if delivery.Attempt >= 5 || delivery.Attempt < 1 {
		result.Status = "abandoned"
	}
	if result.Status == "failed" {
		delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
		delay := delays[delivery.Attempt-1]
		if provider != nil && provider.RetryAfter > delay {
			delay = provider.RetryAfter
		}
		result.NextAttemptAt = now.Add(delay)
	}
	return result
}
