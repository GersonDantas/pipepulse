package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrOAuthRequestNotFound = errors.New("oauth request not found")
var ErrOAuthExchangeNotFound = errors.New("oauth exchange not found")
var ErrSessionNotFound = errors.New("session not found")

type OAuthRequest struct {
	ID                     string
	StateHash              []byte
	CodeVerifierCiphertext []byte
	ExchangeVerifierHash   []byte
	RedirectURI            string
	ExpiresAt              time.Time
}

type Principal struct {
	UserID      string `json:"user_id"`
	WorkspaceID string `json:"workspace_id"`
	GithubLogin string `json:"github_login"`
	PlanCode    string `json:"plan"`
}

type SessionTokens struct {
	AccessTokenHash  []byte
	RefreshTokenHash []byte
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

type AuthStore interface {
	CreateOAuthRequest(context.Context, OAuthRequest) error
	ConsumeOAuthRequest(context.Context, []byte, time.Time) (OAuthRequest, error)
	CompleteOAuthRequest(context.Context, string, []byte, int64, string, time.Time) error
	ExchangeOAuthSession(context.Context, []byte, []byte, SessionTokens, time.Time) (Principal, error)
	RotateSession(context.Context, []byte, SessionTokens, time.Time) (Principal, error)
	Authenticate(context.Context, []byte, time.Time) (Principal, error)
	RevokeSession(context.Context, []byte, time.Time) error
}

func (store *PostgresStore) CreateOAuthRequest(ctx context.Context, request OAuthRequest) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO oauth_requests (state_hash, code_verifier_ciphertext, exchange_verifier_hash, redirect_uri, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, request.StateHash, request.CodeVerifierCiphertext, request.ExchangeVerifierHash, request.RedirectURI, request.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert oauth request: %w", err)
	}
	return nil
}

func (store *PostgresStore) ConsumeOAuthRequest(ctx context.Context, stateHash []byte, now time.Time) (OAuthRequest, error) {
	var request OAuthRequest
	err := store.pool.QueryRow(ctx, `
		UPDATE oauth_requests
		SET used_at = $2
		WHERE state_hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING id::text, state_hash, code_verifier_ciphertext, redirect_uri, expires_at
	`, stateHash, now).Scan(&request.ID, &request.StateHash, &request.CodeVerifierCiphertext, &request.RedirectURI, &request.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthRequest{}, ErrOAuthRequestNotFound
	}
	if err != nil {
		return OAuthRequest{}, fmt.Errorf("consume oauth request: %w", err)
	}
	return request, nil
}

func (store *PostgresStore) CompleteOAuthRequest(ctx context.Context, id string, exchangeHash []byte, githubUserID int64, githubLogin string, expiresAt time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin oauth completion: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, githubUserID); err != nil {
		return fmt.Errorf("lock oauth identity: %w", err)
	}
	result, err := tx.Exec(ctx, `
		UPDATE oauth_requests AS request
		SET exchange_token_hash = $2, github_user_id = $3, github_login = $4, exchange_expires_at = $5
		WHERE request.id = $1 AND request.used_at IS NOT NULL AND request.exchange_token_hash IS NULL
			AND NOT EXISTS (
				SELECT 1 FROM oauth_account_deletions AS deletion
				WHERE deletion.github_user_id = $3 AND request.created_at <= deletion.deleted_at
			)
	`, id, exchangeHash, githubUserID, githubLogin, expiresAt)
	if err != nil {
		return fmt.Errorf("complete oauth request: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrOAuthRequestNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit oauth completion: %w", err)
	}
	return nil
}

func (store *PostgresStore) ExchangeOAuthSession(ctx context.Context, exchangeHash, verifierHash []byte, tokens SessionTokens, now time.Time) (Principal, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Principal{}, fmt.Errorf("begin oauth exchange: %w", err)
	}
	defer tx.Rollback(context.Background())

	var requestID string
	var githubUserID int64
	var githubLogin string
	err = tx.QueryRow(ctx, `
		SELECT id::text, github_user_id, github_login
		FROM oauth_requests
		WHERE exchange_token_hash = $1 AND exchange_verifier_hash = $2
			AND exchanged_at IS NULL AND exchange_expires_at > $3
		FOR UPDATE
	`, exchangeHash, verifierHash, now).Scan(&requestID, &githubUserID, &githubLogin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrOAuthExchangeNotFound
	}
	if err != nil {
		return Principal{}, fmt.Errorf("lock oauth exchange: %w", err)
	}

	var principal Principal
	err = tx.QueryRow(ctx, `
		INSERT INTO users (github_user_id, github_login)
		VALUES ($1, $2)
		ON CONFLICT (github_user_id) DO UPDATE
		SET github_login = EXCLUDED.github_login, updated_at = now()
		RETURNING id::text, github_login
	`, githubUserID, githubLogin).Scan(&principal.UserID, &principal.GithubLogin)
	if err != nil {
		return Principal{}, fmt.Errorf("upsert oauth user: %w", err)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO workspaces (owner_user_id)
		VALUES ($1)
		ON CONFLICT (owner_user_id) DO UPDATE SET updated_at = now()
		RETURNING id::text, plan_code
	`, principal.UserID).Scan(&principal.WorkspaceID, &principal.PlanCode)
	if err != nil {
		return Principal{}, fmt.Errorf("upsert oauth workspace: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sessions (
			user_id, workspace_id, access_token_hash, refresh_token_hash,
			access_expires_at, refresh_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, principal.UserID, principal.WorkspaceID, tokens.AccessTokenHash, tokens.RefreshTokenHash,
		tokens.AccessExpiresAt, tokens.RefreshExpiresAt); err != nil {
		return Principal{}, fmt.Errorf("insert oauth session: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_requests SET exchanged_at = $2 WHERE id = $1`, requestID, now); err != nil {
		return Principal{}, fmt.Errorf("mark oauth exchange used: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Principal{}, fmt.Errorf("commit oauth exchange: %w", err)
	}
	return principal, nil
}

func (store *PostgresStore) RotateSession(ctx context.Context, refreshHash []byte, tokens SessionTokens, now time.Time) (Principal, error) {
	var principal Principal
	err := store.pool.QueryRow(ctx, `
		WITH rotated AS (
			UPDATE sessions
			SET access_token_hash = $2, refresh_token_hash = $3,
				access_expires_at = $4, refresh_expires_at = $5, updated_at = $6
			WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND refresh_expires_at > $6
			RETURNING user_id, workspace_id
		)
		SELECT rotated.user_id::text, rotated.workspace_id::text, users.github_login, workspaces.plan_code
		FROM rotated
		JOIN users ON users.id = rotated.user_id
		JOIN workspaces ON workspaces.id = rotated.workspace_id
	`, refreshHash, tokens.AccessTokenHash, tokens.RefreshTokenHash, tokens.AccessExpiresAt,
		tokens.RefreshExpiresAt, now).Scan(&principal.UserID, &principal.WorkspaceID, &principal.GithubLogin, &principal.PlanCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrSessionNotFound
	}
	if err != nil {
		return Principal{}, fmt.Errorf("rotate session: %w", err)
	}
	return principal, nil
}

func (store *PostgresStore) Authenticate(ctx context.Context, accessHash []byte, now time.Time) (Principal, error) {
	var principal Principal
	err := store.pool.QueryRow(ctx, `
		SELECT sessions.user_id::text, sessions.workspace_id::text, users.github_login, workspaces.plan_code
		FROM sessions
		JOIN users ON users.id = sessions.user_id
		JOIN workspaces ON workspaces.id = sessions.workspace_id
		WHERE sessions.access_token_hash = $1 AND sessions.revoked_at IS NULL
			AND sessions.access_expires_at > $2
	`, accessHash, now).Scan(&principal.UserID, &principal.WorkspaceID, &principal.GithubLogin, &principal.PlanCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrSessionNotFound
	}
	if err != nil {
		return Principal{}, fmt.Errorf("authenticate session: %w", err)
	}
	return principal, nil
}

func (store *PostgresStore) RevokeSession(ctx context.Context, accessHash []byte, now time.Time) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = $2, updated_at = $2
		WHERE access_token_hash = $1 AND revoked_at IS NULL AND access_expires_at > $2
	`, accessHash, now)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrSessionNotFound
	}
	return nil
}
