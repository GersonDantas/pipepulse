package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrProductNotFound = errors.New("product resource not found")
var ErrRepositoryLimit = errors.New("repository limit reached")
var ErrRepositoryExists = errors.New("repository already exists")
var ErrDeviceTokenInUse = errors.New("device token already in use")

type CreateWorkflowInput struct {
	GithubWorkflowID int64  `json:"github_workflow_id"`
	Name             string `json:"name"`
}

type CreateRepositoryInput struct {
	GithubRepositoryID int64               `json:"github_repository_id"`
	Owner              string              `json:"owner"`
	Name               string              `json:"name"`
	Workflow           CreateWorkflowInput `json:"workflow"`
}

type ProductWorkflow struct {
	ID               string `json:"id"`
	GithubWorkflowID int64  `json:"github_workflow_id"`
	Name             string `json:"name"`
}

type ProductPipelineState struct {
	Status         string    `json:"status"`
	Conclusion     string    `json:"conclusion"`
	WorkflowRunID  int64     `json:"workflow_run_id"`
	RunAttempt     int       `json:"run_attempt"`
	EventTimestamp time.Time `json:"event_timestamp"`
	Branch         string    `json:"branch"`
	HeadSHA        string    `json:"head_sha"`
	RunURL         string    `json:"run_url"`
}

type ProductRepository struct {
	ID                 string                `json:"id"`
	GithubRepositoryID int64                 `json:"github_repository_id"`
	Owner              string                `json:"owner"`
	Name               string                `json:"name"`
	EndpointID         string                `json:"-"`
	WebhookURL         string                `json:"webhook_url"`
	WebhookSecret      string                `json:"webhook_secret,omitempty"`
	Workflow           ProductWorkflow       `json:"workflow"`
	State              *ProductPipelineState `json:"state"`
	CreatedAt          time.Time             `json:"created_at"`
}

type Failure struct {
	ID              string    `json:"id"`
	RepositoryID    string    `json:"repository_id"`
	RepositoryOwner string    `json:"repository_owner"`
	RepositoryName  string    `json:"repository_name"`
	WorkflowID      string    `json:"workflow_id"`
	WorkflowName    string    `json:"workflow_name"`
	WorkflowRunID   int64     `json:"workflow_run_id"`
	RunAttempt      int       `json:"run_attempt"`
	Conclusion      string    `json:"conclusion"`
	EventTimestamp  time.Time `json:"event_timestamp"`
	Branch          string    `json:"branch"`
	HeadSHA         string    `json:"head_sha"`
	RunURL          string    `json:"run_url"`
}

type FailureCursor struct {
	EventTimestamp time.Time `json:"event_timestamp"`
	ID             string    `json:"id"`
}

type ProductStore interface {
	CreateRepository(context.Context, string, CreateRepositoryInput, []byte, int) (ProductRepository, error)
	ListRepositories(context.Context, string) ([]ProductRepository, error)
	GetRepository(context.Context, string, string) (ProductRepository, error)
	RotateRepositorySecret(context.Context, string, string, []byte) error
	DeleteRepository(context.Context, string, string) error
	ListFailures(context.Context, string, *FailureCursor, int) ([]Failure, error)
	PutDevice(context.Context, Principal, []byte, []byte, string) error
	DeleteDevice(context.Context, string) error
	DeleteAccount(context.Context, string) error
}

func (store *PostgresStore) CreateRepository(ctx context.Context, workspaceID string, input CreateRepositoryInput, secretCiphertext []byte, limit int) (ProductRepository, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ProductRepository{}, fmt.Errorf("begin repository creation: %w", err)
	}
	defer tx.Rollback(context.Background())

	var planCode string
	if err := tx.QueryRow(ctx, `SELECT plan_code FROM workspaces WHERE id = $1 FOR UPDATE`, workspaceID).Scan(&planCode); errors.Is(err, pgx.ErrNoRows) {
		return ProductRepository{}, ErrProductNotFound
	} else if err != nil {
		return ProductRepository{}, fmt.Errorf("lock workspace: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repositories WHERE workspace_id = $1 AND deleted_at IS NULL`, workspaceID).Scan(&count); err != nil {
		return ProductRepository{}, fmt.Errorf("count repositories: %w", err)
	}
	if count >= limit {
		return ProductRepository{}, ErrRepositoryLimit
	}

	var result ProductRepository
	err = tx.QueryRow(ctx, `
		INSERT INTO repositories (
			workspace_id, github_repository_id, owner, name, webhook_secret_ciphertext
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, github_repository_id, owner, name, endpoint_id::text, created_at
	`, workspaceID, input.GithubRepositoryID, input.Owner, input.Name, secretCiphertext).Scan(
		&result.ID, &result.GithubRepositoryID, &result.Owner, &result.Name, &result.EndpointID, &result.CreatedAt,
	)
	if isUniqueViolation(err) {
		return ProductRepository{}, ErrRepositoryExists
	}
	if err != nil {
		return ProductRepository{}, fmt.Errorf("insert repository: %w", err)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO monitored_workflows (repository_id, github_workflow_id, name)
		VALUES ($1, $2, $3)
		RETURNING id::text, github_workflow_id, name
	`, result.ID, input.Workflow.GithubWorkflowID, input.Workflow.Name).Scan(
		&result.Workflow.ID, &result.Workflow.GithubWorkflowID, &result.Workflow.Name,
	)
	if err != nil {
		return ProductRepository{}, fmt.Errorf("insert monitored workflow: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductRepository{}, fmt.Errorf("commit repository creation: %w", err)
	}
	return result, nil
}

func (store *PostgresStore) ListRepositories(ctx context.Context, workspaceID string) ([]ProductRepository, error) {
	rows, err := store.pool.Query(ctx, productRepositoryQuery+`
		WHERE repository.workspace_id = $1 AND repository.deleted_at IS NULL AND workflow.active
		ORDER BY repository.created_at, repository.id
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()
	result := make([]ProductRepository, 0)
	for rows.Next() {
		repository, err := scanProductRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		result = append(result, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories: %w", err)
	}
	return result, nil
}

func (store *PostgresStore) GetRepository(ctx context.Context, workspaceID, id string) (ProductRepository, error) {
	if !isUUID(id) {
		return ProductRepository{}, ErrProductNotFound
	}
	result, err := scanProductRepository(store.pool.QueryRow(ctx, productRepositoryQuery+`
		WHERE repository.workspace_id = $1 AND repository.id = $2
			AND repository.deleted_at IS NULL AND workflow.active
	`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProductRepository{}, ErrProductNotFound
	}
	if err != nil {
		return ProductRepository{}, fmt.Errorf("get repository: %w", err)
	}
	return result, nil
}

func (store *PostgresStore) RotateRepositorySecret(ctx context.Context, workspaceID, id string, ciphertext []byte) error {
	if !isUUID(id) {
		return ErrProductNotFound
	}
	result, err := store.pool.Exec(ctx, `
		UPDATE repositories
		SET webhook_secret_ciphertext = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND deleted_at IS NULL
	`, workspaceID, id, ciphertext)
	if err != nil {
		return fmt.Errorf("rotate repository secret: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrProductNotFound
	}
	return nil
}

func (store *PostgresStore) DeleteRepository(ctx context.Context, workspaceID, id string) error {
	if !isUUID(id) {
		return ErrProductNotFound
	}
	result, err := store.pool.Exec(ctx, `DELETE FROM repositories WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrProductNotFound
	}
	return nil
}

func (store *PostgresStore) ListFailures(ctx context.Context, workspaceID string, cursor *FailureCursor, limit int) ([]Failure, error) {
	var cursorTime any
	var cursorID any
	if cursor != nil {
		cursorTime = cursor.EventTimestamp
		cursorID = cursor.ID
	}
	rows, err := store.pool.Query(ctx, `
		SELECT failure.id::text, repository.id::text, repository.owner, repository.name,
			workflow.id::text, workflow.name, failure.workflow_run_id, failure.run_attempt,
			failure.conclusion, failure.event_timestamp, failure.branch, failure.head_sha, failure.run_url
		FROM pipeline_failures AS failure
		JOIN repositories AS repository ON repository.id = failure.repository_id
		JOIN monitored_workflows AS workflow ON workflow.id = failure.monitored_workflow_id
		WHERE repository.workspace_id = $1 AND repository.deleted_at IS NULL
			AND ($2::timestamptz IS NULL OR failure.event_timestamp < $2
				OR (failure.event_timestamp = $2 AND failure.id::text < $3))
		ORDER BY failure.event_timestamp DESC, failure.id DESC
		LIMIT $4
	`, workspaceID, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list failures: %w", err)
	}
	defer rows.Close()
	result := make([]Failure, 0)
	for rows.Next() {
		var failure Failure
		if err := rows.Scan(
			&failure.ID, &failure.RepositoryID, &failure.RepositoryOwner, &failure.RepositoryName,
			&failure.WorkflowID, &failure.WorkflowName, &failure.WorkflowRunID, &failure.RunAttempt,
			&failure.Conclusion, &failure.EventTimestamp, &failure.Branch, &failure.HeadSHA, &failure.RunURL,
		); err != nil {
			return nil, fmt.Errorf("scan failure: %w", err)
		}
		failure.EventTimestamp = failure.EventTimestamp.UTC()
		result = append(result, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failures: %w", err)
	}
	return result, nil
}

func (store *PostgresStore) PutDevice(ctx context.Context, principal Principal, tokenHash, ciphertext []byte, platform string) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin device registration: %w", err)
	}
	defer tx.Rollback(context.Background())
	var lockedUserID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id = $1 FOR UPDATE`, principal.UserID).Scan(&lockedUserID); errors.Is(err, pgx.ErrNoRows) {
		return ErrProductNotFound
	} else if err != nil {
		return fmt.Errorf("lock device user: %w", err)
	}
	var existingUserID string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM devices WHERE token_hash = $1`, tokenHash).Scan(&existingUserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("find device token: %w", err)
	}
	if err == nil && existingUserID != principal.UserID {
		return ErrDeviceTokenInUse
	}
	if _, err := tx.Exec(ctx, `
		UPDATE devices SET active = false, disabled_at = now(), updated_at = now()
		WHERE user_id = $1 AND active
	`, principal.UserID); err != nil {
		return fmt.Errorf("disable previous device: %w", err)
	}
	if existingUserID == principal.UserID {
		_, err = tx.Exec(ctx, `
			UPDATE devices
			SET workspace_id = $2, token_ciphertext = $3, platform = $4,
				active = true, disabled_at = NULL, updated_at = now()
			WHERE user_id = $1 AND token_hash = $5
		`, principal.UserID, principal.WorkspaceID, ciphertext, platform, tokenHash)
	} else {
		_, err = tx.Exec(ctx, `
			INSERT INTO devices (user_id, workspace_id, token_hash, token_ciphertext, platform)
			VALUES ($1, $2, $3, $4, $5)
		`, principal.UserID, principal.WorkspaceID, tokenHash, ciphertext, platform)
	}
	if isUniqueViolation(err) {
		return ErrDeviceTokenInUse
	}
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit device registration: %w", err)
	}
	return nil
}

func (store *PostgresStore) DeleteDevice(ctx context.Context, userID string) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE devices SET active = false, disabled_at = now(), updated_at = now()
		WHERE user_id = $1 AND active
	`, userID)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	return nil
}

func (store *PostgresStore) DeleteAccount(ctx context.Context, userID string) error {
	result, err := store.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrProductNotFound
	}
	return nil
}

const productRepositoryQuery = `
	SELECT repository.id::text, repository.github_repository_id, repository.owner, repository.name,
		repository.endpoint_id::text, repository.created_at,
		workflow.id::text, workflow.github_workflow_id, workflow.name,
		state.status, state.conclusion, state.workflow_run_id, state.run_attempt,
		state.event_timestamp, state.branch, state.head_sha, state.run_url
	FROM repositories AS repository
	JOIN monitored_workflows AS workflow ON workflow.repository_id = repository.id
	LEFT JOIN pipeline_states AS state ON state.monitored_workflow_id = workflow.id
`

type rowScanner interface {
	Scan(...any) error
}

func scanProductRepository(row rowScanner) (ProductRepository, error) {
	var result ProductRepository
	var status, conclusion, branch, headSHA, runURL *string
	var workflowRunID *int64
	var runAttempt *int
	var eventTimestamp *time.Time
	err := row.Scan(
		&result.ID, &result.GithubRepositoryID, &result.Owner, &result.Name, &result.EndpointID, &result.CreatedAt,
		&result.Workflow.ID, &result.Workflow.GithubWorkflowID, &result.Workflow.Name,
		&status, &conclusion, &workflowRunID, &runAttempt, &eventTimestamp, &branch, &headSHA, &runURL,
	)
	if err != nil {
		return ProductRepository{}, err
	}
	result.CreatedAt = result.CreatedAt.UTC()
	if status != nil {
		result.State = &ProductPipelineState{
			Status: *status, Conclusion: valueOrEmpty(conclusion), WorkflowRunID: *workflowRunID,
			RunAttempt: *runAttempt, EventTimestamp: eventTimestamp.UTC(), Branch: valueOrEmpty(branch),
			HeadSHA: valueOrEmpty(headSHA), RunURL: valueOrEmpty(runURL),
		}
	}
	return result, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(value) == nil && id.Valid
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
