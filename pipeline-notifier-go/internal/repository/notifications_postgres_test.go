package repository_test

import (
	"context"
	"io"
	"log/slog"
	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/notifications"
	"pipeline-notifier/internal/processor"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
	"pipeline-notifier/internal/testsupport"
	"sync"
	"testing"
	"time"
)

func TestNotificationOutboxClaimAndRecovery(t *testing.T) {
	ctx := context.Background()
	url := testsupport.StartPostgres(t)
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repoID, workflowID := seedWorkspaceWithDevice(t, ctx, pool)
	store := repository.NewPostgresStore(pool)
	event := workflowEvent("outbox-1", models.PipelineStatusFailed, time.Now().UTC())
	event.Conclusion = "failure"
	enqueueAndAssociate(t, ctx, pool, store, event, repoID, workflowID)
	tx, err := store.BeginProcessing(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	failureID, _, err := tx.CreateFailure(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.CreateNotifications(ctx, failureID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Second)
	claimed, err := store.ClaimNotification(ctx, now)
	if err != nil || claimed == nil {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	if claimed.Attempt != 1 || claimed.FailureID != failureID || claimed.RepositoryID != repoID {
		t.Fatalf("claim = %#v", claimed)
	}
	other, err := repository.NewPostgresStore(pool).ClaimNotification(ctx, now)
	if err != nil || other != nil {
		t.Fatalf("parallel claim = %#v, %v", other, err)
	}
	recovered, err := store.ClaimNotification(ctx, now.Add(time.Minute))
	if err != nil || recovered == nil || recovered.Attempt != 2 {
		t.Fatalf("recovered = %#v, %v", recovered, err)
	}
	if err := store.FinishNotification(ctx, *claimed, repository.NotificationResult{Status: "sent", CompletedAt: now}); err == nil {
		t.Fatal("stale completion accepted")
	}
	if err := store.FinishNotification(ctx, *claimed, repository.NotificationResult{Status: "abandoned", CompletedAt: now, DisableDevice: true}); err == nil {
		t.Fatal("stale invalid-token completion accepted")
	}
	var active bool
	if err := pool.QueryRow(ctx, `SELECT active FROM devices WHERE id = $1`, claimed.DeviceID).Scan(&active); err != nil || !active {
		t.Fatalf("stale completion disabled device: %v", err)
	}
	if err := store.FinishNotification(ctx, *recovered, repository.NotificationResult{Status: "sent", CompletedAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	other, err = store.ClaimNotification(ctx, now.Add(24*time.Hour))
	if err != nil || other != nil {
		t.Fatalf("sent reclaimed = %#v, %v", other, err)
	}

	t.Run("crash recovery preserves retry intervals", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'pending',attempt_count = 0,next_attempt_at = $1`, now); err != nil {
			t.Fatal(err)
		}
		claimTime := now
		for index, delay := range []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, time.Minute} {
			d, err := store.ClaimNotification(ctx, claimTime)
			if err != nil || d == nil || d.Attempt != index+1 {
				t.Fatalf("claim = %#v,%v", d, err)
			}
			var next time.Time
			if err := pool.QueryRow(ctx, `SELECT next_attempt_at FROM notification_deliveries`).Scan(&next); err != nil {
				t.Fatal(err)
			}
			if next.Sub(claimTime).Round(time.Millisecond) != delay {
				t.Fatalf("attempt %d lease delay = %s, want %s", index+1, next.Sub(claimTime), delay)
			}
			if d, err := store.ClaimNotification(ctx, claimTime.Add(delay-time.Second)); err != nil || d != nil {
				t.Fatalf("early recovery = %#v,%v", d, err)
			}
			claimTime = claimTime.Add(delay)
		}
		if d, err := store.ClaimNotification(ctx, claimTime); err != nil || d != nil {
			t.Fatalf("sixth recovery = %#v,%v", d, err)
		}
	})
	t.Run("concurrent claims reserve once", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'pending',attempt_count = 0,next_attempt_at = $1`, now); err != nil {
			t.Fatal(err)
		}
		var group sync.WaitGroup
		claims := make(chan *repository.Notification, 2)
		claimErrors := make(chan error, 2)
		for range 2 {
			group.Add(1)
			go func() {
				defer group.Done()
				d, err := store.ClaimNotification(ctx, now)
				claims <- d
				claimErrors <- err
			}()
		}
		group.Wait()
		close(claims)
		close(claimErrors)
		for err := range claimErrors {
			if err != nil {
				t.Fatal(err)
			}
		}
		count := 0
		for d := range claims {
			if d != nil {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("concurrent claims = %d", count)
		}
	})
	t.Run("crash on final attempt is abandoned", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'sending',attempt_count = 5,next_attempt_at = $1`, now); err != nil {
			t.Fatal(err)
		}
		if d, err := store.ClaimNotification(ctx, now); err != nil || d != nil {
			t.Fatalf("sixth claim = %#v,%v", d, err)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM notification_deliveries`).Scan(&status); err != nil || status != "abandoned" {
			t.Fatalf("status = %s,%v", status, err)
		}
	})
	t.Run("token invalidation abandons all outstanding device notifications", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'pending',attempt_count = 0,next_attempt_at = $1`, now); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO notification_deliveries (pipeline_failure_id,failure_deduplication_key,device_id,next_attempt_at) VALUES ($1,'second-run',$2,$3)`, failureID, claimed.DeviceID, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		d, err := store.ClaimNotification(ctx, now)
		if err != nil || d == nil {
			t.Fatalf("claim = %#v,%v", d, err)
		}
		if err := store.FinishNotification(ctx, *d, repository.NotificationResult{Status: "abandoned", CompletedAt: now, ErrorCode: "unregistered", DisableDevice: true}); err != nil {
			t.Fatal(err)
		}
		var outstanding int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE status != 'abandoned'`).Scan(&outstanding); err != nil || outstanding != 0 {
			t.Fatalf("outstanding = %d,%v", outstanding, err)
		}
		if err := pool.QueryRow(ctx, `SELECT active FROM devices WHERE id = $1`, d.DeviceID).Scan(&active); err != nil || active {
			t.Fatalf("invalid device active = %v,%v", active, err)
		}
	})
	t.Run("removed failure is never sent", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE devices SET active = true,disabled_at = NULL; UPDATE notification_deliveries SET status = 'pending',attempt_count = 0,next_attempt_at = now(); DELETE FROM pipeline_failures`); err != nil {
			t.Fatal(err)
		}
		if d, err := store.ClaimNotification(ctx, time.Now().Add(time.Minute)); err != nil || d != nil {
			t.Fatalf("orphan claim = %#v,%v", d, err)
		}
	})
}

type capturingSender struct{ messages chan notifications.Message }

func (s capturingSender) Send(_ context.Context, message notifications.Message) error {
	s.messages <- message
	return nil
}

func TestNotificationWorkerPipelineIntegration(t *testing.T) {
	url := testsupport.StartPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repoID, workflowID := seedWorkspaceWithDevice(t, ctx, pool)
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	token, _ := box.Encrypt([]byte("integration-token"))
	if _, err := pool.Exec(ctx, `UPDATE devices SET token_ciphertext = $1`, token); err != nil {
		t.Fatal(err)
	}
	store := repository.NewPostgresStore(pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eventProcessor := processor.New(store, logger)
	event := workflowEvent("push-delivery", models.PipelineStatusFailed, time.Now().UTC())
	event.Conclusion = "failure"
	enqueueAndAssociate(t, ctx, pool, store, event, repoID, workflowID)
	if err := eventProcessor.Process(ctx, event); err != nil {
		t.Fatal(err)
	}
	sender := capturingSender{messages: make(chan notifications.Message, 5)}
	workerContext, stop := context.WithCancel(ctx)
	done := notifications.NewWorker(store, sender, box, 10*time.Millisecond, logger).Start(workerContext)
	defer func() { stop(); <-done }()
	waitMessage := func() notifications.Message {
		t.Helper()
		select {
		case message := <-sender.messages:
			return message
		case <-time.After(3 * time.Second):
			t.Fatal("push not sent")
			return notifications.Message{}
		}
	}
	first := waitMessage()
	if first.Token != "integration-token" || first.RepositoryID != repoID || first.FailureID == "" {
		t.Fatal("incorrect outbox payload")
	}
	event.DeliveryID = "redelivery-same-run"
	event.Timestamp = event.Timestamp.Add(time.Second)
	enqueueAndAssociate(t, ctx, pool, store, event, repoID, workflowID)
	if err := eventProcessor.Process(ctx, event); err != nil {
		t.Fatal(err)
	}
	assertCount(t, ctx, pool, "notification_deliveries", 1)
	event.DeliveryID = "different-run"
	event.WorkflowRunID++
	event.Timestamp = event.Timestamp.Add(time.Second)
	enqueueAndAssociate(t, ctx, pool, store, event, repoID, workflowID)
	if err := eventProcessor.Process(ctx, event); err != nil {
		t.Fatal(err)
	}
	second := waitMessage()
	if second.NotificationID == first.NotificationID || second.FailureID == first.FailureID {
		t.Fatal("new run lost its distinct notification")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var sent int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE status = 'sent'`).Scan(&sent); err != nil {
			t.Fatal(err)
		}
		if sent == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("outbox results not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	restartedContext, stopRestarted := context.WithCancel(ctx)
	restarted := notifications.NewWorker(repository.NewPostgresStore(pool), sender, box, 10*time.Millisecond, logger).Start(restartedContext)
	select {
	case <-sender.messages:
		t.Error("completed outbox resent after restart")
	case <-time.After(50 * time.Millisecond):
	}
	stopRestarted()
	<-restarted
}
