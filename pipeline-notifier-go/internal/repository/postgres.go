package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pipeline-notifier/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (store *PostgresStore) Enqueue(ctx context.Context, event models.Event) (bool, error) {
	result, err := store.pool.Exec(ctx, `
		INSERT INTO webhook_deliveries (
			delivery_id, github_repository_id, github_workflow_id, workflow_run_id,
			run_attempt, status, conclusion, event_timestamp, branch, head_sha, run_url
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (delivery_id) DO NOTHING
	`, event.DeliveryID, event.RepositoryID, event.WorkflowID, event.WorkflowRunID,
		event.RunAttempt, event.Status, event.Conclusion, event.Timestamp, event.Branch, event.SHA, event.RunURL)
	if err != nil {
		return false, fmt.Errorf("persist webhook delivery: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

func (store *PostgresStore) NextPending(ctx context.Context) (*models.Event, error) {
	row := store.pool.QueryRow(ctx, `
		WITH next AS (
			SELECT delivery_id
			FROM webhook_deliveries
			WHERE processed_at IS NULL
			ORDER BY received_at, delivery_id
			LIMIT 1
		)
		UPDATE webhook_deliveries AS delivery
		SET processing_attempts = processing_attempts + 1
		FROM next
		WHERE delivery.delivery_id = next.delivery_id
		RETURNING delivery.delivery_id, delivery.github_repository_id,
			delivery.github_workflow_id, delivery.workflow_run_id, delivery.run_attempt,
			delivery.status, delivery.conclusion, delivery.event_timestamp,
			delivery.branch, delivery.head_sha, delivery.run_url
	`)

	var event models.Event
	if err := row.Scan(
		&event.DeliveryID,
		&event.RepositoryID,
		&event.WorkflowID,
		&event.WorkflowRunID,
		&event.RunAttempt,
		&event.Status,
		&event.Conclusion,
		&event.Timestamp,
		&event.Branch,
		&event.SHA,
		&event.RunURL,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load pending delivery: %w", err)
	}
	event.Timestamp = event.Timestamp.UTC()
	return &event, nil
}

func (store *PostgresStore) BeginProcessing(ctx context.Context, event models.Event) (EventTransaction, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin event transaction: %w", err)
	}
	rollback := func(cause error) (EventTransaction, error) {
		_ = tx.Rollback(ctx)
		return nil, cause
	}

	var repositoryID, workflowID *string
	var processed bool
	err = tx.QueryRow(ctx, `
		SELECT repository_id::text, monitored_workflow_id::text, processed_at IS NOT NULL
		FROM webhook_deliveries
		WHERE delivery_id = $1
		FOR UPDATE
	`, event.DeliveryID).Scan(&repositoryID, &workflowID, &processed)
	if errors.Is(err, pgx.ErrNoRows) {
		return rollback(ErrDeliveryNotFound)
	}
	if err != nil {
		return rollback(fmt.Errorf("lock webhook delivery: %w", err))
	}
	if processed {
		return rollback(ErrDeliveryAlreadyProcessed)
	}

	transaction := &postgresEventTransaction{
		tx:           tx,
		event:        event,
		repositoryID: repositoryID,
		workflowID:   workflowID,
	}
	current, err := transaction.loadCurrentState(ctx)
	if err != nil {
		return rollback(err)
	}
	transaction.current = current
	return transaction, nil
}

func (store *PostgresStore) GetState(ctx context.Context, pipelineID string) (*State, error) {
	parts := strings.Split(pipelineID, ":")
	if len(parts) != 2 {
		return nil, nil
	}
	repositoryID, repositoryErr := strconv.ParseInt(parts[0], 10, 64)
	workflowID, workflowErr := strconv.ParseInt(parts[1], 10, 64)
	if repositoryErr != nil || workflowErr != nil {
		return nil, nil
	}

	state := &State{PipelineID: pipelineID}
	err := store.pool.QueryRow(ctx, `
		SELECT github_repository_id, github_workflow_id, workflow_run_id, run_attempt,
			status, conclusion, event_timestamp, last_delivery_id
		FROM pipeline_states
		WHERE github_repository_id = $1 AND github_workflow_id = $2
		ORDER BY updated_at DESC
		LIMIT 1
	`, repositoryID, workflowID).Scan(
		&state.RepositoryID,
		&state.WorkflowID,
		&state.WorkflowRunID,
		&state.RunAttempt,
		&state.Status,
		&state.Conclusion,
		&state.Timestamp,
		&state.LastDeliveryID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pipeline state: %w", err)
	}
	state.Timestamp = state.Timestamp.UTC()
	return state, nil
}

type postgresEventTransaction struct {
	tx           pgx.Tx
	event        models.Event
	repositoryID *string
	workflowID   *string
	current      *State
	finished     bool
}

func (transaction *postgresEventTransaction) CurrentState() *State {
	return transaction.current
}

func (transaction *postgresEventTransaction) loadCurrentState(ctx context.Context) (*State, error) {
	state := &State{PipelineID: transaction.event.PipelineKey()}
	query := `
		SELECT github_repository_id, github_workflow_id, workflow_run_id, run_attempt,
			status, conclusion, event_timestamp, last_delivery_id
		FROM pipeline_states
		WHERE monitored_workflow_id = $1
		FOR UPDATE
	`
	argument := any(transaction.workflowID)
	if transaction.workflowID == nil {
		query = `
			SELECT github_repository_id, github_workflow_id, workflow_run_id, run_attempt,
				status, conclusion, event_timestamp, last_delivery_id
			FROM pipeline_states
			WHERE monitored_workflow_id IS NULL
				AND github_repository_id = $1 AND github_workflow_id = $2
			FOR UPDATE
		`
		argument = transaction.event.RepositoryID
	}

	var err error
	if transaction.workflowID == nil {
		err = transaction.tx.QueryRow(ctx, query, argument, transaction.event.WorkflowID).Scan(
			&state.RepositoryID, &state.WorkflowID, &state.WorkflowRunID, &state.RunAttempt,
			&state.Status, &state.Conclusion, &state.Timestamp, &state.LastDeliveryID,
		)
	} else {
		err = transaction.tx.QueryRow(ctx, query, argument).Scan(
			&state.RepositoryID, &state.WorkflowID, &state.WorkflowRunID, &state.RunAttempt,
			&state.Status, &state.Conclusion, &state.Timestamp, &state.LastDeliveryID,
		)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load pipeline state: %w", err)
	}
	state.Timestamp = state.Timestamp.UTC()
	return state, nil
}

func (transaction *postgresEventTransaction) SaveState(ctx context.Context, state State) error {
	if transaction.workflowID == nil {
		_, err := transaction.tx.Exec(ctx, `
			INSERT INTO pipeline_states (
				repository_id, monitored_workflow_id, github_repository_id, github_workflow_id,
				workflow_run_id, run_attempt, status, conclusion, event_timestamp,
				branch, head_sha, run_url, last_delivery_id
			) VALUES (NULL, NULL, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (github_repository_id, github_workflow_id) WHERE monitored_workflow_id IS NULL
			DO UPDATE SET workflow_run_id = EXCLUDED.workflow_run_id,
				run_attempt = EXCLUDED.run_attempt, status = EXCLUDED.status,
				conclusion = EXCLUDED.conclusion, event_timestamp = EXCLUDED.event_timestamp,
				branch = EXCLUDED.branch, head_sha = EXCLUDED.head_sha,
				run_url = EXCLUDED.run_url, last_delivery_id = EXCLUDED.last_delivery_id,
				updated_at = now()
		`, state.RepositoryID, state.WorkflowID, state.WorkflowRunID, state.RunAttempt,
			state.Status, state.Conclusion, state.Timestamp, transaction.event.Branch,
			transaction.event.SHA, transaction.event.RunURL, state.LastDeliveryID)
		if err != nil {
			return fmt.Errorf("save unassociated pipeline state: %w", err)
		}
		return nil
	}

	_, err := transaction.tx.Exec(ctx, `
		INSERT INTO pipeline_states (
			repository_id, monitored_workflow_id, github_repository_id, github_workflow_id,
			workflow_run_id, run_attempt, status, conclusion, event_timestamp,
			branch, head_sha, run_url, last_delivery_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (monitored_workflow_id) WHERE monitored_workflow_id IS NOT NULL
		DO UPDATE SET workflow_run_id = EXCLUDED.workflow_run_id,
			run_attempt = EXCLUDED.run_attempt, status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion, event_timestamp = EXCLUDED.event_timestamp,
			branch = EXCLUDED.branch, head_sha = EXCLUDED.head_sha,
			run_url = EXCLUDED.run_url, last_delivery_id = EXCLUDED.last_delivery_id,
			updated_at = now()
	`, transaction.repositoryID, transaction.workflowID, state.RepositoryID, state.WorkflowID,
		state.WorkflowRunID, state.RunAttempt, state.Status, state.Conclusion, state.Timestamp,
		transaction.event.Branch, transaction.event.SHA, transaction.event.RunURL, state.LastDeliveryID)
	if err != nil {
		return fmt.Errorf("save pipeline state: %w", err)
	}
	return nil
}

func (transaction *postgresEventTransaction) CreateFailure(ctx context.Context, event models.Event) (string, bool, error) {
	var failureID string
	err := transaction.tx.QueryRow(ctx, `
		INSERT INTO pipeline_failures (
			repository_id, monitored_workflow_id, github_repository_id, github_workflow_id,
			workflow_run_id, run_attempt, conclusion, event_timestamp, branch, head_sha, run_url
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT DO NOTHING
		RETURNING id::text
	`, transaction.repositoryID, transaction.workflowID, event.RepositoryID, event.WorkflowID,
		event.WorkflowRunID, event.RunAttempt, event.Conclusion, event.Timestamp,
		event.Branch, event.SHA, event.RunURL).Scan(&failureID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("create pipeline failure: %w", err)
	}
	return failureID, true, nil
}

func (transaction *postgresEventTransaction) CreateNotifications(ctx context.Context, failureID string) (int64, error) {
	if transaction.repositoryID == nil {
		return 0, nil
	}
	result, err := transaction.tx.Exec(ctx, `
		INSERT INTO notification_deliveries (pipeline_failure_id, failure_deduplication_key, device_id)
		SELECT $1::uuid, $1::uuid, device.id
		FROM repositories AS repository
		JOIN devices AS device ON device.workspace_id = repository.workspace_id AND device.active
		WHERE repository.id = $2
		ON CONFLICT (failure_deduplication_key, device_id) DO NOTHING
	`, failureID, transaction.repositoryID)
	if err != nil {
		return 0, fmt.Errorf("create notification deliveries: %w", err)
	}
	return result.RowsAffected(), nil
}

type RetentionResult struct {
	WebhookDeliveries      int64
	PipelineFailures       int64
	NotificationDeliveries int64
}

func (store *PostgresStore) Cleanup(ctx context.Context, now time.Time) (RetentionResult, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return RetentionResult{}, fmt.Errorf("begin retention transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	notificationResult, err := tx.Exec(ctx, `
		DELETE FROM notification_deliveries
		WHERE status IN ('sent', 'abandoned') AND updated_at < $1
	`, now.Add(-30*24*time.Hour))
	if err != nil {
		return RetentionResult{}, fmt.Errorf("delete expired notification results: %w", err)
	}
	failureResult, err := tx.Exec(ctx, `
		DELETE FROM pipeline_failures AS failure
		WHERE failure.event_timestamp < $1
			AND (
				failure.repository_id IS NULL
				OR EXISTS (
					SELECT 1
					FROM repositories AS repository
					JOIN workspaces AS workspace ON workspace.id = repository.workspace_id
					WHERE repository.id = failure.repository_id AND workspace.plan_code = 'free'
				)
			)
	`, now.Add(-7*24*time.Hour))
	if err != nil {
		return RetentionResult{}, fmt.Errorf("delete expired pipeline failures: %w", err)
	}
	webhookResult, err := tx.Exec(ctx, `
		DELETE FROM webhook_deliveries
		WHERE processed_at IS NOT NULL AND processed_at < $1
	`, now.Add(-7*24*time.Hour))
	if err != nil {
		return RetentionResult{}, fmt.Errorf("delete expired webhook deliveries: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RetentionResult{}, fmt.Errorf("commit retention transaction: %w", err)
	}
	return RetentionResult{
		WebhookDeliveries:      webhookResult.RowsAffected(),
		PipelineFailures:       failureResult.RowsAffected(),
		NotificationDeliveries: notificationResult.RowsAffected(),
	}, nil
}

func (transaction *postgresEventTransaction) CompleteDelivery(ctx context.Context, ignoredReason string) error {
	var reason any
	if ignoredReason != "" {
		reason = ignoredReason
	}
	_, err := transaction.tx.Exec(ctx, `
		UPDATE webhook_deliveries
		SET processed_at = now(), ignored_reason = $2, last_error = NULL
		WHERE delivery_id = $1
	`, transaction.event.DeliveryID, reason)
	if err != nil {
		return fmt.Errorf("complete webhook delivery: %w", err)
	}
	return nil
}

func (transaction *postgresEventTransaction) Commit(ctx context.Context) error {
	if transaction.finished {
		return nil
	}
	transaction.finished = true
	if err := transaction.tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit event transaction: %w", err)
	}
	return nil
}

func (transaction *postgresEventTransaction) Rollback(ctx context.Context) error {
	if transaction.finished {
		return nil
	}
	transaction.finished = true
	err := transaction.tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}
