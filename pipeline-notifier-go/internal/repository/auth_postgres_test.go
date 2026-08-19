package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"pipeline-notifier/internal/database"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/testsupport"
)

func TestPostgresAuthLifecycleUsesSingleUseTokens(t *testing.T) {
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
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)

	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash:              []byte("state-hash"),
		CodeVerifierCiphertext: []byte("ciphertext"),
		RedirectURI:            "com.pipepulse.app://oauth",
		ExpiresAt:              now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest() error = %v", err)
	}
	request, err := store.ConsumeOAuthRequest(ctx, []byte("state-hash"), now)
	if err != nil {
		t.Fatalf("ConsumeOAuthRequest() error = %v", err)
	}
	if _, err := store.ConsumeOAuthRequest(ctx, []byte("state-hash"), now); !errors.Is(err, repository.ErrOAuthRequestNotFound) {
		t.Fatalf("second ConsumeOAuthRequest() error = %v, want ErrOAuthRequestNotFound", err)
	}
	if err := store.CompleteOAuthRequest(ctx, request.ID, []byte("exchange-hash"), 1, "octocat", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("CompleteOAuthRequest() error = %v", err)
	}
	first := repository.SessionTokens{
		AccessTokenHash: []byte("access-1"), RefreshTokenHash: []byte("refresh-1"),
		AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	principal, err := store.ExchangeOAuthSession(ctx, []byte("exchange-hash"), first, now)
	if err != nil {
		t.Fatalf("ExchangeOAuthSession() error = %v", err)
	}
	if principal.UserID == "" || principal.WorkspaceID == "" || principal.GithubLogin != "octocat" || principal.PlanCode != "free" {
		t.Fatalf("principal = %#v, want automatic Free workspace", principal)
	}
	if _, err := store.ExchangeOAuthSession(ctx, []byte("exchange-hash"), first, now); !errors.Is(err, repository.ErrOAuthExchangeNotFound) {
		t.Fatalf("second ExchangeOAuthSession() error = %v, want ErrOAuthExchangeNotFound", err)
	}
	if _, err := store.Authenticate(ctx, first.AccessTokenHash, now); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	second := repository.SessionTokens{
		AccessTokenHash: []byte("access-2"), RefreshTokenHash: []byte("refresh-2"),
		AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if _, err := store.RotateSession(ctx, first.RefreshTokenHash, second, now); err != nil {
		t.Fatalf("RotateSession() error = %v", err)
	}
	if _, err := store.Authenticate(ctx, first.AccessTokenHash, now); !errors.Is(err, repository.ErrSessionNotFound) {
		t.Fatalf("old access token error = %v, want ErrSessionNotFound", err)
	}
	if _, err := store.RotateSession(ctx, first.RefreshTokenHash, second, now); !errors.Is(err, repository.ErrSessionNotFound) {
		t.Fatalf("old refresh token error = %v, want ErrSessionNotFound", err)
	}
	if err := store.RevokeSession(ctx, second.AccessTokenHash, now); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := store.Authenticate(ctx, second.AccessTokenHash, now); !errors.Is(err, repository.ErrSessionNotFound) {
		t.Fatalf("revoked access token error = %v, want ErrSessionNotFound", err)
	}
}
