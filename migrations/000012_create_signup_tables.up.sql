-- 000012_create_signup_tables.up.sql
-- Signup identity, magic-link token, and API key registry tables.
-- Part of the free-key-issuance epic (#69).

-- Ensure pgcrypto is available for gen_random_uuid().
-- Repeated here so existing databases that ran 000001 before pgcrypto was added
-- also get the extension without requiring a re-migration.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- api_identities: one row per unique email address (verified or pending).
CREATE TABLE api_identities (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               TEXT NOT NULL,
    email_verified_at   TIMESTAMPTZ,
    ip_hash             TEXT,           -- SHA-256 of signup IP (privacy-safe telemetry)
    ua_hash             TEXT,           -- SHA-256 of user-agent at signup
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX api_identities_email_idx ON api_identities (LOWER(email));

-- magic_link_tokens: one-time, TTL-bound tokens for email verification.
CREATE TABLE magic_link_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID NOT NULL REFERENCES api_identities (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,          -- SHA-256 of the raw token (never stored plain)
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,            -- NULL = unused; non-NULL = consumed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX magic_link_tokens_hash_idx ON magic_link_tokens (token_hash);
-- Fast expiry pruning: used by the cleanup job or periodic cron.
CREATE INDEX magic_link_tokens_expires_idx ON magic_link_tokens (expires_at)
    WHERE used_at IS NULL;

-- api_keys_registry: maps verified identities to their Unkey-issued key IDs.
CREATE TABLE api_keys_registry (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     UUID NOT NULL REFERENCES api_identities (id) ON DELETE CASCADE,
    provider_key_id TEXT NOT NULL,      -- Unkey key.id returned on creation
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

-- Enforce one-active-key-per-identity policy.
CREATE UNIQUE INDEX api_keys_registry_one_active_idx
    ON api_keys_registry (identity_id)
    WHERE status = 'active';

CREATE INDEX api_keys_registry_identity_idx ON api_keys_registry (identity_id);
