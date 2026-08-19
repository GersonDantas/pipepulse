package handlers_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/handlers"
	"pipeline-notifier/internal/processor"
	"pipeline-notifier/internal/queue"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/router"
	"pipeline-notifier/internal/secrets"
	"pipeline-notifier/internal/services"
	"pipeline-notifier/internal/testsupport"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGithubWebhookPersistsBeforeAcceptedAndProcessesToFeed(t *testing.T) {
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

	secret := []byte("repository-webhook-secret")
	secretBox, err := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	ciphertext, err := secretBox.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	repositoryID, workflowID, endpointID := seedWebhook(t, ctx, pool, ciphertext)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := repository.NewPostgresStore(pool)
	eventQueue := queue.New(store, time.Hour, logger)
	service := services.NewWebhookService(eventQueue, store, secretBox)
	handler := handlers.New(service, store, logger)
	httpRouter := router.SetupRouter(handler, nil, nil, logger)
	body := `{"repository":{"id":10},"workflow_run":{"id":20,"workflow_id":30,"run_attempt":1,"status":"completed","conclusion":"failure","updated_at":"2026-08-18T18:00:00Z","head_branch":"main","head_sha":"abc123","html_url":"https://github.com/acme/api/actions/runs/20"}}`

	invalid := signedWebhookRequest(endpointID, "delivery-invalid", body, []byte("wrong-secret"))
	invalidRecorder := httptest.NewRecorder()
	httpRouter.ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d, want %d", invalidRecorder.Code, http.StatusUnauthorized)
	}
	assertDeliveryCount(t, ctx, pool, "delivery-invalid", 0)

	request := signedWebhookRequest(endpointID, "delivery-1", body, secret)
	recorder := httptest.NewRecorder()
	httpRouter.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var persistedRepositoryID, persistedWorkflowID string
	var pending bool
	if err := pool.QueryRow(ctx, `
		SELECT repository_id::text, monitored_workflow_id::text, processed_at IS NULL
		FROM webhook_deliveries WHERE delivery_id = 'delivery-1'
	`).Scan(&persistedRepositoryID, &persistedWorkflowID, &pending); err != nil {
		t.Fatalf("query accepted delivery: %v", err)
	}
	if persistedRepositoryID != repositoryID || persistedWorkflowID != workflowID || !pending {
		t.Fatalf("persisted delivery = %q/%q pending=%t, want associated pending delivery", persistedRepositoryID, persistedWorkflowID, pending)
	}

	duplicateRecorder := httptest.NewRecorder()
	httpRouter.ServeHTTP(duplicateRecorder, signedWebhookRequest(endpointID, "delivery-1", body, secret))
	if duplicateRecorder.Code != http.StatusAccepted {
		t.Fatalf("duplicate status = %d, want %d", duplicateRecorder.Code, http.StatusAccepted)
	}
	assertDeliveryCount(t, ctx, pool, "delivery-1", 1)

	event, err := store.NextPending(ctx)
	if err != nil || event == nil {
		t.Fatalf("NextPending() = %#v, %v, want delivery", event, err)
	}
	if err := processor.New(store, logger).Process(ctx, *event); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	assertTableCount(t, ctx, pool, "pipeline_states", 1)
	assertTableCount(t, ctx, pool, "pipeline_failures", 1)
	var conclusion, branch, sha, runURL string
	if err := pool.QueryRow(ctx, `
		SELECT conclusion, branch, head_sha, run_url FROM pipeline_failures
	`).Scan(&conclusion, &branch, &sha, &runURL); err != nil {
		t.Fatalf("query failure feed: %v", err)
	}
	if conclusion != "failure" || branch != "main" || sha != "abc123" || !strings.HasSuffix(runURL, "/20") {
		t.Fatalf("failure feed = %q/%q/%q/%q, want GitHub run details", conclusion, branch, sha, runURL)
	}
}

func seedWebhook(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ciphertext []byte) (string, string, string) {
	t.Helper()
	var userID, workspaceID, repositoryID, workflowID, endpointID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (github_user_id, github_login) VALUES (1, 'octocat') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (owner_user_id) VALUES ($1) RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO repositories (workspace_id, github_repository_id, owner, name, webhook_secret_ciphertext)
		VALUES ($1, 10, 'acme', 'api', $2) RETURNING id, endpoint_id
	`, workspaceID, ciphertext).Scan(&repositoryID, &endpointID); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO monitored_workflows (repository_id, github_workflow_id, name)
		VALUES ($1, 30, 'CI') RETURNING id
	`, repositoryID).Scan(&workflowID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	return repositoryID, workflowID, endpointID
}

func signedWebhookRequest(endpointID, deliveryID, body string, secret []byte) *http.Request {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(body))
	request := httptest.NewRequest(http.MethodPost, "/webhooks/github/"+endpointID, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(digest.Sum(nil)))
	request.Header.Set("X-GitHub-Delivery", deliveryID)
	request.Header.Set("X-GitHub-Event", "workflow_run")
	return request
}

func assertDeliveryCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, deliveryID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE delivery_id = $1`, deliveryID).Scan(&got); err != nil {
		t.Fatalf("count delivery: %v", err)
	}
	if got != want {
		t.Fatalf("delivery %q count = %d, want %d", deliveryID, got, want)
	}
}

func assertTableCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
