package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrWebhookEndpointNotFound = errors.New("webhook endpoint not found")
var ErrMonitoredWorkflowNotFound = errors.New("monitored workflow not found")

type WebhookEndpoint struct {
	RepositoryID            string
	GithubRepositoryID      int64
	WebhookSecretCiphertext []byte
}

func (store *PostgresStore) GetWebhookEndpoint(ctx context.Context, endpointID string) (*WebhookEndpoint, error) {
	var endpoint WebhookEndpoint
	err := store.pool.QueryRow(ctx, `
		SELECT id::text, github_repository_id, webhook_secret_ciphertext
		FROM repositories
		WHERE endpoint_id::text = $1 AND deleted_at IS NULL
	`, endpointID).Scan(&endpoint.RepositoryID, &endpoint.GithubRepositoryID, &endpoint.WebhookSecretCiphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWebhookEndpointNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get webhook endpoint: %w", err)
	}
	return &endpoint, nil
}

func (store *PostgresStore) GetMonitoredWorkflow(ctx context.Context, repositoryID string, githubWorkflowID int64) (string, error) {
	var workflowID string
	err := store.pool.QueryRow(ctx, `
		SELECT id::text
		FROM monitored_workflows
		WHERE repository_id = $1 AND github_workflow_id = $2 AND active
	`, repositoryID, githubWorkflowID).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMonitoredWorkflowNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get monitored workflow: %w", err)
	}
	return workflowID, nil
}
