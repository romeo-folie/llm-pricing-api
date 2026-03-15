-- Ensure pgcrypto is available for gen_random_uuid() on DBs that ran 000001
-- before pgcrypto was added. IF NOT EXISTS makes this fully idempotent.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE webhooks (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_hash TEXT        NOT NULL,
    url         TEXT        NOT NULL,
    secret      TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_webhooks_api_key_hash ON webhooks (api_key_hash);
CREATE INDEX idx_webhooks_deleted_at   ON webhooks (deleted_at) WHERE deleted_at IS NULL;
