-- +goose Up
-- +goose StatementBegin
-- One-time authorization codes for the browser login redirect flow (PKCE).
-- Only the SHA-256 hash of the code is stored. A code is single use (used_at)
-- and short lived (expires_at).
CREATE TABLE auth_codes (
    code_hash      VARCHAR(64) PRIMARY KEY,
    user_id        TEXT NOT NULL,
    idp            TEXT NOT NULL,
    service        TEXT NOT NULL,
    redirect_uri   TEXT NOT NULL,
    code_challenge VARCHAR(128) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL,
    used_at        TIMESTAMPTZ
);

CREATE INDEX idx_auth_codes_expires_at ON auth_codes (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_auth_codes_expires_at;
DROP TABLE IF EXISTS auth_codes;
-- +goose StatementEnd
