-- Migration 000012: identity schema for free API key issuance (Epic #69)
-- Tables: api_identities, magic_link_tokens, api_keys_registry

-- ── api_identities ───────────────────────────────────────────────────────────
-- One row per verified email identity. email is lowercased/trimmed before insert.
CREATE TABLE IF NOT EXISTS api_identities (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             TEXT        NOT NULL,
    email_verified_at TIMESTAMPTZ,
    -- Hashed telemetry — sha256 of IP / UA; never stored raw.
    signup_ip_hash    TEXT,
    signup_ua_hash    TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT api_identities_email_unique UNIQUE (email),
    CONSTRAINT api_identities_email_nonempty CHECK (email <> '')
);

CREATE INDEX IF NOT EXISTS api_identities_email_idx ON api_identities (email);

-- Trigger: keep updated_at current on every UPDATE.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

CREATE TRIGGER api_identities_updated_at
    BEFORE UPDATE ON api_identities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ── magic_link_tokens ─────────────────────────────────────────────────────────
-- One-time tokens for email verification. token_hash = sha256(raw_token).
-- used_at is set on first successful verify; subsequent attempts are rejected.
CREATE TABLE IF NOT EXISTS magic_link_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID        NOT NULL REFERENCES api_identities(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,               -- NULL = unused
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT magic_link_tokens_hash_unique UNIQUE (token_hash)
);

-- Fast lookup by token hash during verification.
CREATE INDEX IF NOT EXISTS magic_link_tokens_hash_idx    ON magic_link_tokens (token_hash);
-- Expiry pruning: delete tokens WHERE expires_at < NOW().
CREATE INDEX IF NOT EXISTS magic_link_tokens_expiry_idx  ON magic_link_tokens (expires_at);
-- Per-identity listing (e.g. rate-limit: how many tokens issued today?).
CREATE INDEX IF NOT EXISTS magic_link_tokens_identity_idx ON magic_link_tokens (identity_id, created_at DESC);

-- ── api_keys_registry ─────────────────────────────────────────────────────────
-- Maps identities → Unkey key IDs. Status: 'active' | 'revoked'.
-- One active key per identity enforced by partial unique index.
CREATE TABLE IF NOT EXISTS api_keys_registry (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     UUID        NOT NULL REFERENCES api_identities(id) ON DELETE CASCADE,
    provider_key_id TEXT        NOT NULL,   -- Unkey key ID
    status          TEXT        NOT NULL DEFAULT 'active'
                                CHECK (status IN ('active', 'revoked')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT api_keys_registry_provider_key_unique UNIQUE (provider_key_id)
);

-- Enforce one active key per identity at the database level.
CREATE UNIQUE INDEX IF NOT EXISTS api_keys_registry_one_active_per_identity
    ON api_keys_registry (identity_id)
    WHERE status = 'active';

-- Fast lookup by identity (key management, dashboard).
CREATE INDEX IF NOT EXISTS api_keys_registry_identity_idx ON api_keys_registry (identity_id, created_at DESC);
