package repository_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/testsupport"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryRecorder struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (recorder *queryRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	recorder.mu.Lock()
	recorder.sql = data.SQL
	recorder.args = append([]any(nil), data.Args...)
	recorder.mu.Unlock()
	return ctx
}

func (*queryRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (recorder *queryRecorder) reset() {
	recorder.mu.Lock()
	recorder.sql = ""
	recorder.args = nil
	recorder.mu.Unlock()
}

func (recorder *queryRecorder) last() (string, []any) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.sql, append([]any(nil), recorder.args...)
}

func TestGetWebhookEndpointUsesUniqueIndex(t *testing.T) {
	databaseURL := testsupport.StartPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	recorder := &queryRecorder{}
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	configuration.ConnConfig.Tracer = recorder
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	defer pool.Close()

	var userID, workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (github_user_id, github_login) VALUES (1, 'octocat') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (owner_user_id) VALUES ($1) RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	const endpointID = "00000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx, `
		INSERT INTO repositories (
			workspace_id, github_repository_id, owner, name, endpoint_id, webhook_secret_ciphertext
		)
		SELECT $1, id, 'owner', 'repo-' || id, CASE WHEN id = 1 THEN $2::uuid ELSE gen_random_uuid() END, decode('01', 'hex')
		FROM generate_series(1, 10000) AS id
	`, workspaceID, endpointID); err != nil {
		t.Fatalf("insert repositories: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE repositories`); err != nil {
		t.Fatalf("analyze repositories: %v", err)
	}

	recorder.reset()
	if _, err := repository.NewPostgresStore(pool).GetWebhookEndpoint(ctx, endpointID); err != nil {
		t.Fatalf("GetWebhookEndpoint() error = %v", err)
	}
	query, args := recorder.last()
	var plan []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (COSTS OFF, FORMAT JSON) "+query, args...).Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN GetWebhookEndpoint() error = %v", err)
	}
	if !strings.Contains(string(plan), `"Index Name": "repositories_endpoint_id_key"`) {
		t.Fatalf("GetWebhookEndpoint() plan = %s, want repositories_endpoint_id_key", plan)
	}
	if _, err := repository.NewPostgresStore(pool).GetWebhookEndpoint(ctx, "not-a-uuid"); !errors.Is(err, repository.ErrWebhookEndpointNotFound) {
		t.Fatalf("GetWebhookEndpoint(invalid UUID) error = %v, want ErrWebhookEndpointNotFound", err)
	}
}
