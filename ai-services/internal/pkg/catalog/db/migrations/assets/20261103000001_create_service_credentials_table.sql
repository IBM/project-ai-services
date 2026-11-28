-- +goose Up
-- +goose StatementBegin
-- Per-deployment credentials that let one service (the caller, for example chat) obtain
-- short-lived tokens for another service. Only the SHA-256 hash of the secret is stored.
-- Several rows per caller can be valid at once, which is how a credential is rotated:
-- a new one is created on redeploy and the old one stays valid until expires_at.
-- grants is what the credential may obtain, fixed when it is issued: {"<audience>": ["<role>", ...]}.
-- A later change to a service's asset does not widen or narrow a credential that already exists.
CREATE TABLE service_credentials (
    id            UUID PRIMARY KEY,
    caller        TEXT NOT NULL,
    deployment_id TEXT NOT NULL,
    secret_hash   VARCHAR(64) NOT NULL,
    grants        JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ
);

CREATE INDEX idx_service_credentials_expires_at ON service_credentials (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_service_credentials_expires_at;
DROP TABLE IF EXISTS service_credentials;
-- +goose StatementEnd
