-- +goose Up
-- +goose StatementBegin
-- Signing keys used by the catalog to issue ES256 service tokens.
-- The private key is stored AES-256-GCM encrypted (DB_ENCRYPTION_KEY); only the
-- public half is ever published (JWKS). A key with retired_at IS NULL is active.
CREATE TABLE signing_keys (
    kid             VARCHAR(64) PRIMARY KEY,
    private_key_enc TEXT NOT NULL,
    public_key_pem  TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at      TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS signing_keys;
-- +goose StatementEnd
