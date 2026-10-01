-- +goose Up
ALTER TABLE oauth_requests ADD COLUMN exchange_verifier_hash BYTEA;

CREATE TABLE oauth_account_deletions (
    github_user_id BIGINT PRIMARY KEY,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

-- +goose Down
DROP TABLE IF EXISTS oauth_account_deletions;
ALTER TABLE oauth_requests DROP COLUMN IF EXISTS exchange_verifier_hash;
