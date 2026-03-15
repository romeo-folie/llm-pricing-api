-- Migration 000012: identity schema for free API key issuance (Epic #69)
-- Tables: api_identities, magic_link_tokens, api_keys_registry
-- Requires: 000003 (set_updated_at function), pgcrypto (for gen_random_uuid)

-- Belt-and-suspenders: guarantee pgcrypto is present on every upgrade path.
-- Fresh installs get it from 000001. Existing DBs that already applied 000001
-- before pgcrypto was declared there (and 000007 before 000007 was patched) may
-- have missed both earlier guards; this declaration closes that gap.
-- All three declarations are IF NOT EXISTS — fully idempotent.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ── api_identities ───────────────────────────────────────────────────────────
-- One row per registered email address. Rows are created at signup time with
-- email_verified_at = NULL; verification is a subsequent step.
-- email is normalised (lowercased, whitespace-trimmed) before insert.
CREATE TABLE api_identities (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email             TEXT        NOT NULL,
    email_verified_at TIMESTAMPTZ,
    -- SHA-256 of IP / UA stored for abuse signals; never stored raw.
    signup_ip_hash    TEXT,
    signup_ua_hash    TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT api_identities_email_unique  UNIQUE (email),
    -- Enforce DB-level normalisation: email must be lowercase, trimmed, non-empty.
    CONSTRAINT api_identities_email_lower   CHECK (email = lower(btrim(email))),
    CONSTRAINT api_identities_email_nonempty CHECK (btrim(email) <> '')
);

-- No separate index on email — UNIQUE constraint already creates one.

-- Trigger: keep updated_at current on every UPDATE.
-- set_updated_at() is created by migration 000003 and is shared across tables;
-- we reuse it here rather than redefining it.
-- Named trg_<table>_<purpose> to match the convention in migration 000003.
CREATE TRIGGER trg_api_identities_updated_at
    BEFORE UPDATE ON api_identities
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ── magic_link_tokens ─────────────────────────────────────────────────────────
-- One-time tokens for email verification. token_hash = sha256(raw_token).
-- used_at is set on first successful verify; subsequent attempts are rejected.
CREATE TABLE magic_link_tokens (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id UUID        NOT NULL REFERENCES api_identities(id) ON DELETE CASCADE,
    token_hash  TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,               -- NULL = unused
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT magic_link_tokens_hash_unique UNIQUE (token_hash)
);

-- No separate index on token_hash — UNIQUE constraint already creates one.
-- Expiry pruning: delete tokens WHERE expires_at < NOW().
-- Named idx_<table>_<columns> to match the convention in migrations 000005/000007.
CREATE INDEX idx_magic_link_tokens_expiry   ON magic_link_tokens (expires_at);
-- Per-identity listing (e.g. rate-limit: how many tokens issued today?).
CREATE INDEX idx_magic_link_tokens_identity ON magic_link_tokens (identity_id, created_at DESC);

-- ── api_keys_registry ─────────────────────────────────────────────────────────
-- Maps identities → Unkey key IDs. Status: 'active' | 'revoked'.
-- One active key per identity enforced by partial unique index.
CREATE TABLE api_keys_registry (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     UUID        NOT NULL REFERENCES api_identities(id) ON DELETE CASCADE,
    provider_key_id TEXT        NOT NULL,   -- Unkey key ID
    status          TEXT        NOT NULL DEFAULT 'active'
                                CHECK (status IN ('active', 'revoked')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT api_keys_registry_provider_key_unique   UNIQUE (provider_key_id),
    -- Reject empty/whitespace-only provider_key_id at the DB level.
    CONSTRAINT api_keys_registry_provider_key_nonempty CHECK (btrim(provider_key_id) <> ''),
    -- Enforce consistent status/revoked_at states: active rows must have no
    -- revocation timestamp; revoked rows must have one.
    CONSTRAINT api_keys_registry_status_revoked_at_consistent
        CHECK (
            (status = 'active'  AND revoked_at IS NULL) OR
            (status = 'revoked' AND revoked_at IS NOT NULL)
        )
);

-- Enforce one active key per identity at the database level.
CREATE UNIQUE INDEX api_keys_registry_one_active_per_identity
    ON api_keys_registry (identity_id)
    WHERE status = 'active';

-- Fast lookup by identity (key management, dashboard).
CREATE INDEX idx_api_keys_registry_identity ON api_keys_registry (identity_id, created_at DESC);
