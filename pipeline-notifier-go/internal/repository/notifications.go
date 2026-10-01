package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Notification struct {
	ID              string
	DeviceID        string
	FailureID       string
	RepositoryID    string
	TokenCiphertext []byte
	Attempt         int
}

type NotificationResult struct {
	Status        string
	ErrorCode     string
	CompletedAt   time.Time
	NextAttemptAt time.Time
	DisableDevice bool
}

var ErrNotificationClaimLost = errors.New("notification claim lost")

func (store *PostgresStore) ClaimNotification(ctx context.Context, now time.Time) (*Notification, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin notification claim: %w", err)
	}
	defer tx.Rollback(context.Background())
	// Terminalize invalid recipients and exhausted claims, including a crash on attempt five.
	_, err = tx.Exec(ctx, `
  UPDATE notification_deliveries AS notification
  SET status = 'abandoned', last_error_code = 'recipient_unavailable', updated_at = $1
  WHERE status IN ('pending','failed','sending') AND (
   (attempt_count >= 5 AND next_attempt_at <= $1)
   OR NOT EXISTS (SELECT 1 FROM devices WHERE id = notification.device_id AND active)
   OR NOT EXISTS (
    SELECT 1 FROM pipeline_failures AS failure
    JOIN repositories AS repository ON repository.id = failure.repository_id
    WHERE failure.id = notification.pipeline_failure_id AND repository.deleted_at IS NULL
   )
  )`, now)
	if err != nil {
		return nil, fmt.Errorf("abandon unavailable notifications: %w", err)
	}
	var delivery Notification
	err = tx.QueryRow(ctx, `
  WITH candidate AS (
   SELECT id FROM notification_deliveries
   WHERE status IN ('pending','failed','sending') AND next_attempt_at <= $1 AND attempt_count < 5
   ORDER BY next_attempt_at,id
   FOR UPDATE SKIP LOCKED LIMIT 1
  )
  UPDATE notification_deliveries AS notification
  SET status = 'sending',attempt_count = attempt_count+1,last_attempt_at = $1,
   next_attempt_at = $1::timestamptz + CASE attempt_count
    WHEN 0 THEN interval '1 minute'
    WHEN 1 THEN interval '5 minutes'
    WHEN 2 THEN interval '30 minutes'
    WHEN 3 THEN interval '2 hours'
    ELSE interval '1 minute' END,updated_at = $1
  FROM candidate,devices AS device,pipeline_failures AS failure
  WHERE notification.id = candidate.id AND device.id = notification.device_id AND device.active
   AND failure.id = notification.pipeline_failure_id
  RETURNING notification.id::text,device.id::text,failure.id::text,failure.repository_id::text,
   device.token_ciphertext,notification.attempt_count
 `, now).Scan(&delivery.ID, &delivery.DeviceID, &delivery.FailureID, &delivery.RepositoryID, &delivery.TokenCiphertext, &delivery.Attempt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("claim notification: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit notification claim: %w", err)
	}
	if delivery.ID == "" {
		return nil, nil
	}
	return &delivery, nil
}

func (store *PostgresStore) NotificationActive(ctx context.Context, delivery Notification) (bool, error) {
	var active bool
	err := store.pool.QueryRow(ctx, `
  SELECT EXISTS (
   SELECT 1 FROM notification_deliveries AS notification
   JOIN devices AS device ON device.id = notification.device_id AND device.active
   JOIN pipeline_failures AS failure ON failure.id = notification.pipeline_failure_id
   JOIN repositories AS repository ON repository.id = failure.repository_id AND repository.deleted_at IS NULL
   WHERE notification.id = $1 AND notification.status = 'sending' AND notification.attempt_count = $2
  )`, delivery.ID, delivery.Attempt).Scan(&active)
	return active, err
}

func (store *PostgresStore) FinishNotification(ctx context.Context, delivery Notification, result NotificationResult) error {
	if result.Status != "sent" && result.Status != "failed" && result.Status != "abandoned" {
		return errors.New("invalid notification result")
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin notification completion: %w", err)
	}
	defer tx.Rollback(context.Background())
	// Serialize finishes for this device before applying invalid-token side effects.
	var deviceID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM devices WHERE id = $1 FOR UPDATE`, delivery.DeviceID).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotificationClaimLost
	}
	if err != nil {
		return fmt.Errorf("lock notification device: %w", err)
	}
	var sentAt any
	if result.Status == "sent" {
		sentAt = result.CompletedAt
	}
	next := result.NextAttemptAt
	if next.IsZero() {
		next = result.CompletedAt
	}
	updated, err := tx.Exec(ctx, `
  UPDATE notification_deliveries
  SET status = $3,last_error_code = NULLIF($4,''),updated_at = $5,sent_at = $6,next_attempt_at = $7
  WHERE id = $1 AND status = 'sending' AND attempt_count = $2 AND device_id = $8
 `, delivery.ID, delivery.Attempt, result.Status, result.ErrorCode, result.CompletedAt, sentAt, next, delivery.DeviceID)
	if err != nil {
		return fmt.Errorf("complete notification: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrNotificationClaimLost
	}
	if result.DisableDevice {
		if _, err := tx.Exec(ctx, `UPDATE devices SET active = false,disabled_at = $2,updated_at = $2 WHERE id = $1`, delivery.DeviceID, result.CompletedAt); err != nil {
			return fmt.Errorf("disable invalid device: %w", err)
		}
		if _, err := tx.Exec(ctx, `
   UPDATE notification_deliveries SET status = 'abandoned',last_error_code = 'recipient_inactive',updated_at = $2
   WHERE device_id = $1 AND status IN ('pending','failed','sending')
  `, delivery.DeviceID, result.CompletedAt); err != nil {
			return fmt.Errorf("abandon device notifications: %w", err)
		}
	}
	return tx.Commit(ctx)
}
