package database

import (
	"context"
	"testing"
	"time"

	"pipeline-notifier/internal/testsupport"
)

func TestMigrateCreatesCompleteMVPSchema(t *testing.T) {
	databaseURL := testsupport.StartPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	wantTables := []string{
		"users",
		"workspaces",
		"oauth_requests",
		"sessions",
		"repositories",
		"monitored_workflows",
		"pipeline_states",
		"pipeline_failures",
		"devices",
		"webhook_deliveries",
		"notification_deliveries",
	}
	rows, err := pool.Query(ctx, `
		SELECT tablename
		FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
		ORDER BY tablename
	`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	gotTables := make(map[string]bool)
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		gotTables[table] = true
	}
	for _, table := range wantTables {
		if !gotTables[table] {
			t.Errorf("table %q was not created", table)
		}
	}

	var userID, workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (github_user_id, github_login) VALUES (1, 'octocat') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert schema test user: %v", err)
	}
	expectConstraintViolation(t, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO users (github_user_id, github_login) VALUES (1, 'duplicate')`)
		return err
	})
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (owner_user_id) VALUES ($1) RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert schema test workspace: %v", err)
	}
	expectConstraintViolation(t, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO workspaces (owner_user_id) VALUES ($1)`, userID)
		return err
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO devices (user_id, workspace_id, token_hash, token_ciphertext, platform)
		VALUES ($1, $2, decode('01', 'hex'), decode('02', 'hex'), 'android')
	`, userID, workspaceID); err != nil {
		t.Fatalf("insert schema test device: %v", err)
	}
	expectConstraintViolation(t, func() error {
		_, err := pool.Exec(ctx, `
			INSERT INTO devices (user_id, workspace_id, token_hash, token_ciphertext, platform)
			VALUES ($1, $2, decode('03', 'hex'), decode('04', 'hex'), 'ios')
		`, userID, workspaceID)
		return err
	})
}

func expectConstraintViolation(t *testing.T, operation func() error) {
	t.Helper()
	if err := operation(); err == nil {
		t.Fatal("operation succeeded, want database constraint violation")
	}
}
