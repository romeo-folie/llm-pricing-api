-- Guard for environments that applied 000001 before pgcrypto was declared there.
-- Since golang-migrate does not re-run already-applied migrations, those
-- environments won't have pgcrypto from 000001; this declaration makes it
-- available when 000007 (the first migration using gen_random_uuid()) runs.
-- IF NOT EXISTS is fully idempotent for fresh installs and later migrations.
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
