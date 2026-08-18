-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_user_id BIGINT NOT NULL UNIQUE,
    github_login TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    plan_code TEXT NOT NULL DEFAULT 'free' CHECK (plan_code IN ('free', 'pro', 'team')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, owner_user_id)
);

CREATE TABLE oauth_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    state_hash BYTEA NOT NULL UNIQUE,
    code_verifier_ciphertext BYTEA NOT NULL,
    redirect_uri TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX oauth_requests_expires_at_idx ON oauth_requests (expires_at);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    access_token_hash BYTEA NOT NULL UNIQUE,
    refresh_token_hash BYTEA NOT NULL UNIQUE,
    access_expires_at TIMESTAMPTZ NOT NULL,
    refresh_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, user_id) REFERENCES workspaces(id, owner_user_id) ON DELETE CASCADE
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_refresh_expires_at_idx ON sessions (refresh_expires_at);

CREATE TABLE repositories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    github_repository_id BIGINT NOT NULL,
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    endpoint_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    webhook_secret_ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (workspace_id, owner, name),
    UNIQUE (workspace_id, github_repository_id)
);

CREATE INDEX repositories_workspace_id_idx ON repositories (workspace_id) WHERE deleted_at IS NULL;

CREATE TABLE monitored_workflows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    github_workflow_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, github_workflow_id),
    UNIQUE (id, repository_id)
);

CREATE TABLE pipeline_states (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID REFERENCES repositories(id) ON DELETE CASCADE,
    monitored_workflow_id UUID,
    github_repository_id BIGINT NOT NULL,
    github_workflow_id BIGINT NOT NULL,
    workflow_run_id BIGINT NOT NULL,
    run_attempt INTEGER NOT NULL CHECK (run_attempt > 0),
    status TEXT NOT NULL CHECK (status IN ('running', 'success', 'cancelled', 'failed')),
    conclusion TEXT NOT NULL DEFAULT '',
    event_timestamp TIMESTAMPTZ NOT NULL,
    branch TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    run_url TEXT NOT NULL DEFAULT '',
    last_delivery_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((repository_id IS NULL) = (monitored_workflow_id IS NULL)),
    FOREIGN KEY (monitored_workflow_id, repository_id)
        REFERENCES monitored_workflows(id, repository_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX pipeline_states_workflow_unique
    ON pipeline_states (monitored_workflow_id)
    WHERE monitored_workflow_id IS NOT NULL;
CREATE UNIQUE INDEX pipeline_states_unassociated_unique
    ON pipeline_states (github_repository_id, github_workflow_id)
    WHERE monitored_workflow_id IS NULL;

CREATE TABLE pipeline_failures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID REFERENCES repositories(id) ON DELETE CASCADE,
    monitored_workflow_id UUID,
    github_repository_id BIGINT NOT NULL,
    github_workflow_id BIGINT NOT NULL,
    workflow_run_id BIGINT NOT NULL,
    run_attempt INTEGER NOT NULL CHECK (run_attempt > 0),
    conclusion TEXT NOT NULL,
    event_timestamp TIMESTAMPTZ NOT NULL,
    branch TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    run_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((repository_id IS NULL) = (monitored_workflow_id IS NULL)),
    FOREIGN KEY (monitored_workflow_id, repository_id)
        REFERENCES monitored_workflows(id, repository_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX pipeline_failures_run_unique
    ON pipeline_failures (repository_id, workflow_run_id, run_attempt)
    WHERE repository_id IS NOT NULL;
CREATE UNIQUE INDEX pipeline_failures_unassociated_run_unique
    ON pipeline_failures (github_repository_id, github_workflow_id, workflow_run_id, run_attempt)
    WHERE repository_id IS NULL;
CREATE INDEX pipeline_failures_feed_idx ON pipeline_failures (repository_id, event_timestamp DESC, id DESC);

CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    token_ciphertext BYTEA NOT NULL,
    platform TEXT NOT NULL CHECK (platform IN ('android', 'ios')),
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    FOREIGN KEY (workspace_id, user_id) REFERENCES workspaces(id, owner_user_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX devices_one_active_per_user ON devices (user_id) WHERE active;
CREATE INDEX devices_workspace_active_idx ON devices (workspace_id) WHERE active;

CREATE TABLE webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    repository_id UUID REFERENCES repositories(id) ON DELETE CASCADE,
    monitored_workflow_id UUID,
    github_repository_id BIGINT NOT NULL,
    github_workflow_id BIGINT NOT NULL,
    workflow_run_id BIGINT NOT NULL,
    run_attempt INTEGER NOT NULL CHECK (run_attempt > 0),
    status TEXT NOT NULL CHECK (status IN ('running', 'success', 'cancelled', 'failed')),
    conclusion TEXT NOT NULL DEFAULT '',
    event_timestamp TIMESTAMPTZ NOT NULL,
    branch TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    run_url TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    ignored_reason TEXT,
    processing_attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    CHECK ((repository_id IS NULL) = (monitored_workflow_id IS NULL)),
    FOREIGN KEY (monitored_workflow_id, repository_id)
        REFERENCES monitored_workflows(id, repository_id) ON DELETE CASCADE
);

CREATE INDEX webhook_deliveries_pending_idx
    ON webhook_deliveries (received_at, delivery_id)
    WHERE processed_at IS NULL;
CREATE INDEX webhook_deliveries_processed_at_idx ON webhook_deliveries (processed_at);

CREATE TABLE notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_failure_id UUID REFERENCES pipeline_failures(id) ON DELETE SET NULL,
    failure_deduplication_key TEXT NOT NULL,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'abandoned')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (failure_deduplication_key, device_id)
);

CREATE INDEX notification_deliveries_pending_idx
    ON notification_deliveries (next_attempt_at, id)
    WHERE status IN ('pending', 'failed');
CREATE INDEX notification_deliveries_result_retention_idx
    ON notification_deliveries (updated_at)
    WHERE status IN ('sent', 'abandoned');

-- +goose Down
DROP TABLE IF EXISTS notification_deliveries;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS pipeline_failures;
DROP TABLE IF EXISTS pipeline_states;
DROP TABLE IF EXISTS monitored_workflows;
DROP TABLE IF EXISTS repositories;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS oauth_requests;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS users;
