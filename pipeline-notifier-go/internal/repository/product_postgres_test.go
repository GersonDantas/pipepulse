package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/testsupport"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresProductStoreEnforcesLimitsAndWorkspaceIsolation(t *testing.T) {
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
	store := repository.NewPostgresStore(pool)
	first := seedPrincipal(t, ctx, pool, 1, "octocat")
	second := seedPrincipal(t, ctx, pool, 2, "hubot")

	var created repository.ProductRepository
	for index := 1; index <= 3; index++ {
		created, err = store.CreateRepository(ctx, first.WorkspaceID, repository.CreateRepositoryInput{
			GithubRepositoryID: int64(index), Owner: "acme", Name: fmt.Sprintf("repo-%d", index),
			Workflow: repository.CreateWorkflowInput{GithubWorkflowID: int64(index * 10), Name: "CI"},
		}, []byte{byte(index)}, 3)
		if err != nil {
			t.Fatalf("CreateRepository(%d) error = %v", index, err)
		}
	}
	if _, err := store.CreateRepository(ctx, first.WorkspaceID, repository.CreateRepositoryInput{
		GithubRepositoryID: 4, Owner: "acme", Name: "repo-4",
		Workflow: repository.CreateWorkflowInput{GithubWorkflowID: 40, Name: "CI"},
	}, []byte{4}, 3); !errors.Is(err, repository.ErrRepositoryLimit) {
		t.Fatalf("fourth CreateRepository() error = %v, want ErrRepositoryLimit", err)
	}
	repositories, err := store.ListRepositories(ctx, first.WorkspaceID)
	if err != nil || len(repositories) != 3 {
		t.Fatalf("ListRepositories() = %d, %v", len(repositories), err)
	}
	if _, err := store.GetRepository(ctx, second.WorkspaceID, created.ID); !errors.Is(err, repository.ErrProductNotFound) {
		t.Fatalf("cross-workspace GetRepository() error = %v, want ErrProductNotFound", err)
	}
	if err := store.RotateRepositorySecret(ctx, second.WorkspaceID, created.ID, []byte("new")); !errors.Is(err, repository.ErrProductNotFound) {
		t.Fatalf("cross-workspace RotateRepositorySecret() error = %v, want ErrProductNotFound", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO pipeline_failures (
			repository_id, monitored_workflow_id, github_repository_id, github_workflow_id,
			workflow_run_id, run_attempt, conclusion, event_timestamp
		) VALUES ($1, $2, $3, $4, 100, 1, 'failure', $5)
	`, created.ID, created.Workflow.ID, created.GithubRepositoryID, created.Workflow.GithubWorkflowID, time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("insert failure: %v", err)
	}
	failures, err := store.ListFailures(ctx, first.WorkspaceID, nil, 20)
	if err != nil || len(failures) != 1 || failures[0].RepositoryID != created.ID {
		t.Fatalf("ListFailures() = %#v, %v", failures, err)
	}
	failures, err = store.ListFailures(ctx, second.WorkspaceID, nil, 20)
	if err != nil || len(failures) != 0 {
		t.Fatalf("cross-workspace ListFailures() = %#v, %v", failures, err)
	}

	if err := store.PutDevice(ctx, first, []byte("token-1"), []byte("cipher-1"), "android"); err != nil {
		t.Fatalf("PutDevice(first) error = %v", err)
	}
	if err := store.PutDevice(ctx, first, []byte("token-2"), []byte("cipher-2"), "android"); err != nil {
		t.Fatalf("PutDevice(replace) error = %v", err)
	}
	var activeDevices int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE user_id = $1 AND active`, first.UserID).Scan(&activeDevices); err != nil || activeDevices != 1 {
		t.Fatalf("active devices = %d, %v", activeDevices, err)
	}
	if err := store.PutDevice(ctx, second, []byte("token-2"), []byte("cipher-2"), "ios"); !errors.Is(err, repository.ErrDeviceTokenInUse) {
		t.Fatalf("shared PutDevice() error = %v, want ErrDeviceTokenInUse", err)
	}

	if err := store.DeleteRepository(ctx, first.WorkspaceID, created.ID); err != nil {
		t.Fatalf("DeleteRepository() error = %v", err)
	}
	if _, err := store.GetRepository(ctx, first.WorkspaceID, created.ID); !errors.Is(err, repository.ErrProductNotFound) {
		t.Fatalf("deleted GetRepository() error = %v, want ErrProductNotFound", err)
	}
	if err := store.DeleteAccount(ctx, first.UserID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	var users int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, first.UserID).Scan(&users); err != nil || users != 0 {
		t.Fatalf("deleted users = %d, %v", users, err)
	}
}

func seedPrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, githubID int64, login string) repository.Principal {
	t.Helper()
	var principal repository.Principal
	if err := pool.QueryRow(ctx, `INSERT INTO users (github_user_id, github_login) VALUES ($1, $2) RETURNING id::text`, githubID, login).Scan(&principal.UserID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (owner_user_id) VALUES ($1) RETURNING id::text, plan_code`, principal.UserID).Scan(&principal.WorkspaceID, &principal.PlanCode); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	principal.GithubLogin = login
	return principal
}
