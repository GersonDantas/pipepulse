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
		ExchangeVerifierHash:   []byte("client-verifier"),
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
	if _, err := store.ExchangeOAuthSession(ctx, []byte("exchange-hash"), []byte("wrong-verifier"), first, now); !errors.Is(err, repository.ErrOAuthExchangeNotFound) {
		t.Fatalf("wrong verifier error = %v, want ErrOAuthExchangeNotFound", err)
	}
	principal, err := store.ExchangeOAuthSession(ctx, []byte("exchange-hash"), []byte("client-verifier"), first, now)
	if err != nil {
		t.Fatalf("ExchangeOAuthSession() error = %v", err)
	}
	if principal.UserID == "" || principal.WorkspaceID == "" || principal.GithubLogin != "octocat" || principal.PlanCode != "free" {
		t.Fatalf("principal = %#v, want automatic Free workspace", principal)
	}
	if _, err := store.ExchangeOAuthSession(ctx, []byte("exchange-hash"), []byte("client-verifier"), first, now); !errors.Is(err, repository.ErrOAuthExchangeNotFound) {
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
	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash: []byte("pending-state"), CodeVerifierCiphertext: []byte("ciphertext"),
		ExchangeVerifierHash: []byte("pending-verifier"),
		RedirectURI:          "com.pipepulse.app://oauth", ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest(pending) error = %v", err)
	}
	pending, err := store.ConsumeOAuthRequest(ctx, []byte("pending-state"), now)
	if err != nil {
		t.Fatalf("ConsumeOAuthRequest(pending) error = %v", err)
	}
	if err := store.CompleteOAuthRequest(ctx, pending.ID, []byte("pending-exchange"), 1, "octocat", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("CompleteOAuthRequest(pending) error = %v", err)
	}
	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash: []byte("unidentified-state"), CodeVerifierCiphertext: []byte("ciphertext"),
		ExchangeVerifierHash: []byte("unidentified-verifier"),
		RedirectURI:          "com.pipepulse.app://oauth", ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest(unidentified) error = %v", err)
	}
	unidentified, err := store.ConsumeOAuthRequest(ctx, []byte("unidentified-state"), now)
	if err != nil {
		t.Fatalf("ConsumeOAuthRequest(unidentified) error = %v", err)
	}
	if err := store.DeleteAccount(ctx, principal.UserID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if err := store.CompleteOAuthRequest(ctx, unidentified.ID, []byte("late-exchange"), 1, "octocat", now.Add(5*time.Minute)); !errors.Is(err, repository.ErrOAuthRequestNotFound) {
		t.Fatalf("callback started before deletion error = %v, want ErrOAuthRequestNotFound", err)
	}
	var remainingRequests int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM oauth_requests WHERE github_user_id = 1`).Scan(&remainingRequests); err != nil || remainingRequests != 0 {
		t.Fatalf("OAuth requests after account deletion = %d, %v, want zero", remainingRequests, err)
	}
	if _, err := store.ExchangeOAuthSession(ctx, []byte("pending-exchange"), []byte("pending-verifier"), first, now); !errors.Is(err, repository.ErrOAuthExchangeNotFound) {
		t.Fatalf("pending exchange after account deletion error = %v, want ErrOAuthExchangeNotFound", err)
	}
	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash: []byte("new-login-state"), CodeVerifierCiphertext: []byte("ciphertext"),
		ExchangeVerifierHash: []byte("new-login-verifier"),
		RedirectURI:          "com.pipepulse.app://oauth", ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest(new login) error = %v", err)
	}
	newLogin, err := store.ConsumeOAuthRequest(ctx, []byte("new-login-state"), now)
	if err != nil {
		t.Fatalf("ConsumeOAuthRequest(new login) error = %v", err)
	}
	if err := store.CompleteOAuthRequest(ctx, newLogin.ID, []byte("new-login-exchange"), 1, "octocat", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("new login after account deletion error = %v", err)
	}
	newLoginTokens := repository.SessionTokens{
		AccessTokenHash: []byte("access-3"), RefreshTokenHash: []byte("refresh-3"),
		AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if _, err := store.ExchangeOAuthSession(ctx, []byte("new-login-exchange"), []byte("new-login-verifier"), newLoginTokens, now); err != nil {
		t.Fatalf("new exchange after account deletion error = %v", err)
	}
	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash: []byte("expired-state"), CodeVerifierCiphertext: []byte("ciphertext"),
		RedirectURI: "com.pipepulse.app://oauth", ExpiresAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest(expired) error = %v", err)
	}
	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash: []byte("valid-state"), CodeVerifierCiphertext: []byte("ciphertext"),
		ExchangeVerifierHash: []byte("valid-verifier"),
		RedirectURI:          "com.pipepulse.app://oauth", ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest(valid) error = %v", err)
	}
	valid, err := store.ConsumeOAuthRequest(ctx, []byte("valid-state"), now)
	if err != nil {
		t.Fatalf("ConsumeOAuthRequest(valid) error = %v", err)
	}
	if err := store.CompleteOAuthRequest(ctx, valid.ID, []byte("valid-exchange"), 2, "hubot", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("CompleteOAuthRequest(valid) error = %v", err)
	}
	if _, err := store.Cleanup(ctx, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	var expiredRequests int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM oauth_requests WHERE state_hash = $1`, []byte("expired-state")).Scan(&expiredRequests); err != nil || expiredRequests != 0 {
		t.Fatalf("expired OAuth requests = %d, %v, want zero", expiredRequests, err)
	}
	validTokens := repository.SessionTokens{
		AccessTokenHash: []byte("access-4"), RefreshTokenHash: []byte("refresh-4"),
		AccessExpiresAt: now.Add(17 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if _, err := store.ExchangeOAuthSession(ctx, []byte("valid-exchange"), []byte("valid-verifier"), validTokens, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("valid exchange after cleanup error = %v", err)
	}
	if err := store.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash: []byte("concurrent-state"), CodeVerifierCiphertext: []byte("ciphertext"),
		ExchangeVerifierHash: []byte("concurrent-verifier"),
		RedirectURI:          "com.pipepulse.app://oauth", ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOAuthRequest(concurrent) error = %v", err)
	}
	concurrent, err := store.ConsumeOAuthRequest(ctx, []byte("concurrent-state"), now)
	if err != nil {
		t.Fatalf("ConsumeOAuthRequest(concurrent) error = %v", err)
	}
	if err := store.CompleteOAuthRequest(ctx, concurrent.ID, []byte("concurrent-exchange"), 3, "monalisa", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("CompleteOAuthRequest(concurrent) error = %v", err)
	}
	results := make(chan error, 2)
	for _, tokens := range []repository.SessionTokens{
		{AccessTokenHash: []byte("access-5"), RefreshTokenHash: []byte("refresh-5"), AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)},
		{AccessTokenHash: []byte("access-6"), RefreshTokenHash: []byte("refresh-6"), AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour)},
	} {
		go func(tokens repository.SessionTokens) {
			_, err := store.ExchangeOAuthSession(ctx, []byte("concurrent-exchange"), []byte("concurrent-verifier"), tokens, now)
			results <- err
		}(tokens)
	}
	successes, rejected := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, repository.ErrOAuthExchangeNotFound) {
			rejected++
		} else {
			t.Fatalf("concurrent exchange error = %v", err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("concurrent exchanges = %d successes, %d rejected; want one each", successes, rejected)
	}
	var deletionTime time.Time
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM oauth_account_deletions WHERE github_user_id = 1`).Scan(&deletionTime); err != nil {
		t.Fatalf("load account deletion marker: %v", err)
	}
	if _, err := store.Cleanup(ctx, deletionTime.Add(11*time.Minute)); err != nil {
		t.Fatalf("Cleanup(final) error = %v", err)
	}
	var deletionMarkers int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM oauth_account_deletions`).Scan(&deletionMarkers); err != nil || deletionMarkers != 0 {
		t.Fatalf("obsolete account deletion markers = %d, %v, want zero", deletionMarkers, err)
	}
}
