package repository_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/processor"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/testsupport"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresStoreDurabilityAndAtomicProcessing(t *testing.T) {
	databaseURL := testsupport.StartPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	t.Run("delivery survives store recreation and is globally idempotent", func(t *testing.T) {
		truncate(t, ctx, pool)
		store := repository.NewPostgresStore(pool)
		event := workflowEvent("delivery-durable", models.PipelineStatusRunning, time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC))

		inserted, err := store.Enqueue(ctx, event)
		if err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
		if !inserted {
			t.Fatal("Enqueue() inserted = false, want true")
		}
		inserted, err = store.Enqueue(ctx, event)
		if err != nil {
			t.Fatalf("duplicate Enqueue() error = %v", err)
		}
		if inserted {
			t.Fatal("duplicate Enqueue() inserted = true, want false")
		}

		restartedStore := repository.NewPostgresStore(pool)
		pending, err := restartedStore.NextPending(ctx)
		if err != nil {
			t.Fatalf("NextPending() error = %v", err)
		}
		if pending == nil || pending.DeliveryID != event.DeliveryID {
			t.Fatalf("NextPending() = %#v, want delivery-durable", pending)
		}
	})

	t.Run("processor commits state failure outbox and delivery together", func(t *testing.T) {
		truncate(t, ctx, pool)
		repositoryID, workflowID := seedWorkspaceWithDevice(t, ctx, pool)
		store := repository.NewPostgresStore(pool)
		event := workflowEvent("delivery-failure", models.PipelineStatusFailed, time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC))
		event.Conclusion = "failure"
		event.Branch = "main"
		event.SHA = "abc123"
		event.RunURL = "https://github.com/acme/api/actions/runs/30"
		enqueueAndAssociate(t, ctx, pool, store, event, repositoryID, workflowID)

		eventProcessor := processor.New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := eventProcessor.Process(ctx, event); err != nil {
			t.Fatalf("Process() error = %v", err)
		}

		assertCount(t, ctx, pool, "pipeline_states", 1)
		assertCount(t, ctx, pool, "pipeline_failures", 1)
		assertCount(t, ctx, pool, "notification_deliveries", 1)
		var processed bool
		if err := pool.QueryRow(ctx, `SELECT processed_at IS NOT NULL FROM webhook_deliveries WHERE delivery_id = $1`, event.DeliveryID).Scan(&processed); err != nil {
			t.Fatalf("query processed delivery: %v", err)
		}
		if !processed {
			t.Fatal("delivery remained pending after committed processing")
		}

		repeatedFailure := event
		repeatedFailure.DeliveryID = "delivery-failure-repeat"
		repeatedFailure.Timestamp = repeatedFailure.Timestamp.Add(time.Minute)
		enqueueAndAssociate(t, ctx, pool, store, repeatedFailure, repositoryID, workflowID)
		if err := eventProcessor.Process(ctx, repeatedFailure); err != nil {
			t.Fatalf("Process(repeated failure) error = %v", err)
		}
		assertCount(t, ctx, pool, "pipeline_failures", 1)
		assertCount(t, ctx, pool, "notification_deliveries", 1)

		older := event
		older.DeliveryID = "delivery-older"
		older.Timestamp = event.Timestamp.Add(-time.Minute)
		older.Status = models.PipelineStatusRunning
		older.Conclusion = ""
		enqueueAndAssociate(t, ctx, pool, store, older, repositoryID, workflowID)
		if err := eventProcessor.Process(ctx, older); err != nil {
			t.Fatalf("Process(older) error = %v", err)
		}
		var lastDeliveryID, ignoredReason string
		if err := pool.QueryRow(ctx, `SELECT last_delivery_id FROM pipeline_states`).Scan(&lastDeliveryID); err != nil {
			t.Fatalf("query state: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT ignored_reason FROM webhook_deliveries WHERE delivery_id = $1`, older.DeliveryID).Scan(&ignoredReason); err != nil {
			t.Fatalf("query ignored delivery: %v", err)
		}
		if lastDeliveryID != repeatedFailure.DeliveryID || ignoredReason != "older_event" {
			t.Fatalf("last delivery = %q, ignored reason = %q", lastDeliveryID, ignoredReason)
		}
	})

	t.Run("processor rolls back every write when failure persistence fails", func(t *testing.T) {
		truncate(t, ctx, pool)
		store := repository.NewPostgresStore(pool)
		event := workflowEvent("delivery-rollback", models.PipelineStatusFailed, time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
		event.Conclusion = "failure"
		if _, err := store.Enqueue(ctx, event); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
		if _, err := pool.Exec(ctx, `
			CREATE FUNCTION reject_failure() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				RAISE EXCEPTION 'forced failure';
			END;
			$$;
			CREATE TRIGGER reject_failure BEFORE INSERT ON pipeline_failures
			FOR EACH ROW EXECUTE FUNCTION reject_failure();
		`); err != nil {
			t.Fatalf("create failure trigger: %v", err)
		}

		eventProcessor := processor.New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := eventProcessor.Process(ctx, event); err == nil {
			t.Fatal("Process() error = nil, want forced transaction failure")
		}
		assertCount(t, ctx, pool, "pipeline_states", 0)
		assertCount(t, ctx, pool, "pipeline_failures", 0)
		var pending bool
		if err := pool.QueryRow(ctx, `SELECT processed_at IS NULL FROM webhook_deliveries WHERE delivery_id = $1`, event.DeliveryID).Scan(&pending); err != nil {
			t.Fatalf("query rolled back delivery: %v", err)
		}
		if !pending {
			t.Fatal("delivery was completed despite transaction rollback")
		}
	})

	t.Run("retention preserves notification idempotency for the same run attempt", func(t *testing.T) {
		truncate(t, ctx, pool)
		repositoryID, workflowID := seedWorkspaceWithDevice(t, ctx, pool)
		store := repository.NewPostgresStore(pool)
		now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
		event := workflowEvent("delivery-before-retention", models.PipelineStatusFailed, now.Add(-8*24*time.Hour))
		event.Conclusion = "failure"
		enqueueAndAssociate(t, ctx, pool, store, event, repositoryID, workflowID)

		eventProcessor := processor.New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := eventProcessor.Process(ctx, event); err != nil {
			t.Fatalf("Process() error = %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET processed_at = $1`, event.Timestamp); err != nil {
			t.Fatalf("age processed delivery: %v", err)
		}
		if _, err := store.Cleanup(ctx, now); err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
		assertCount(t, ctx, pool, "pipeline_failures", 0)
		assertCount(t, ctx, pool, "notification_deliveries", 1)

		repeated := event
		repeated.DeliveryID = "delivery-after-retention"
		repeated.Timestamp = event.Timestamp.Add(time.Minute)
		enqueueAndAssociate(t, ctx, pool, store, repeated, repositoryID, workflowID)
		if err := eventProcessor.Process(ctx, repeated); err != nil {
			t.Fatalf("Process(repeated) error = %v", err)
		}

		assertCount(t, ctx, pool, "notification_deliveries", 1)
	})

	t.Run("retention removes only expired durable records", func(t *testing.T) {
		truncate(t, ctx, pool)
		repositoryID, workflowID := seedWorkspaceWithDevice(t, ctx, pool)
		store := repository.NewPostgresStore(pool)
		now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)

		oldProcessed := workflowEvent("delivery-old-processed", models.PipelineStatusFailed, now.Add(-8*24*time.Hour))
		oldProcessed.Conclusion = "failure"
		enqueueAndAssociate(t, ctx, pool, store, oldProcessed, repositoryID, workflowID)
		eventProcessor := processor.New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := eventProcessor.Process(ctx, oldProcessed); err != nil {
			t.Fatalf("Process(old delivery) error = %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET processed_at = $1 WHERE delivery_id = $2`, now.Add(-8*24*time.Hour), oldProcessed.DeliveryID); err != nil {
			t.Fatalf("age processed delivery: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE pipeline_failures SET event_timestamp = $1`, now.Add(-8*24*time.Hour)); err != nil {
			t.Fatalf("age pipeline failure: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE notification_deliveries SET status = 'sent', sent_at = $1, updated_at = $1`, now.Add(-20*24*time.Hour)); err != nil {
			t.Fatalf("age notification result: %v", err)
		}

		pending := workflowEvent("delivery-old-pending", models.PipelineStatusRunning, now.Add(-8*24*time.Hour))
		if _, err := store.Enqueue(ctx, pending); err != nil {
			t.Fatalf("Enqueue(pending) error = %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET received_at = $1 WHERE delivery_id = $2`, now.Add(-8*24*time.Hour), pending.DeliveryID); err != nil {
			t.Fatalf("age pending delivery: %v", err)
		}

		result, err := store.Cleanup(ctx, now)
		if err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}
		if result.WebhookDeliveries != 1 || result.PipelineFailures != 1 || result.NotificationDeliveries != 0 {
			t.Fatalf("Cleanup() result = %#v, want 1 delivery, 1 failure, 0 notification results", result)
		}
		assertCount(t, ctx, pool, "webhook_deliveries", 1)
		assertCount(t, ctx, pool, "pipeline_failures", 0)
		assertCount(t, ctx, pool, "notification_deliveries", 1)
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
		Timestamp:     timestamp,
	}
}

func truncate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		DROP FUNCTION IF EXISTS reject_failure() CASCADE;
		TRUNCATE notification_deliveries, webhook_deliveries, devices, pipeline_failures,
			pipeline_states, monitored_workflows, repositories, sessions, oauth_requests,
			workspaces, users CASCADE;
	`); err != nil {
		t.Fatalf("truncate database: %v", err)
	}
}

func seedWorkspaceWithDevice(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	var userID, workspaceID, repositoryID, workflowID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (github_user_id, github_login) VALUES (1, 'octocat') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (owner_user_id) VALUES ($1) RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO repositories (workspace_id, github_repository_id, owner, name, webhook_secret_ciphertext)
		VALUES ($1, 10, 'acme', 'api', decode('01', 'hex')) RETURNING id
	`, workspaceID).Scan(&repositoryID); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO monitored_workflows (repository_id, github_workflow_id, name)
		VALUES ($1, 20, 'CI') RETURNING id
	`, repositoryID).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO devices (user_id, workspace_id, token_hash, token_ciphertext, platform)
		VALUES ($1, $2, decode('02', 'hex'), decode('03', 'hex'), 'android')
	`, userID, workspaceID); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	return repositoryID, workflowID
}

func enqueueAndAssociate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *repository.PostgresStore, event models.Event, repositoryID, workflowID string) {
	t.Helper()
	if _, err := store.Enqueue(ctx, event); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET repository_id = $2, monitored_workflow_id = $3
		WHERE delivery_id = $1
	`, event.DeliveryID, repositoryID, workflowID); err != nil {
		t.Fatalf("associate delivery: %v", err)
	}
}

func assertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
