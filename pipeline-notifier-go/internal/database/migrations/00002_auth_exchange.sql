-- +goose Up
ALTER TABLE oauth_requests
    ADD COLUMN exchange_token_hash BYTEA,
    ADD COLUMN github_user_id BIGINT,
    ADD COLUMN github_login TEXT,
    ADD COLUMN exchange_expires_at TIMESTAMPTZ,
    ADD COLUMN exchanged_at TIMESTAMPTZ;

CREATE UNIQUE INDEX oauth_requests_exchange_token_hash_idx
    ON oauth_requests (exchange_token_hash)
    WHERE exchange_token_hash IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS oauth_requests_exchange_token_hash_idx;
ALTER TABLE oauth_requests
    DROP COLUMN IF EXISTS exchanged_at,
    DROP COLUMN IF EXISTS exchange_expires_at,
    DROP COLUMN IF EXISTS github_login,
    DROP COLUMN IF EXISTS github_user_id,
    DROP COLUMN IF EXISTS exchange_token_hash;
