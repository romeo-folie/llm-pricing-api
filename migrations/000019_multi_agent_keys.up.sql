-- Migration 000019: per-agent API keys + agent device grants
--
-- Two related changes that together let an agent obtain a key on behalf of a
-- user without the user ever handling the secret:
--
--  1. api_keys_registry no longer permits exactly one active key per identity.
--     That constraint made a second agent's onboarding destructive: it either
--     overwrote the first agent's key or was refused outright. Keys are now
--     per-agent so each can be revoked and attributed independently. The new
--     cap (5 active keys) is enforced atomically inside the INSERT — see
--     signup.InsertKeyWithLabel — never as a read-then-write count, which races.
--
--  2. agent_grants backs the device-authorization flow (RFC 8628 shape) used by
--     POST /auth/agent/device, /auth/agent/approve and /auth/agent/token.
--     device_code_hash = sha256(raw device code); the raw code is returned to
--     the agent exactly once and is the credential that redeems the key, so it
--     is never stored or logged in plaintext.
--
-- Requires: 000012 (api_identities, api_keys_registry), pgcrypto.

-- ── api_keys_registry: per-agent keys ────────────────────────────────────────

-- label is the agent's self-reported client name; it is untrusted input and is
-- length-capped here as a second line of defence behind handler validation.
-- created_via distinguishes magic-link keys from agent-issued ones for audit.
ALTER TABLE api_keys_registry
    ADD COLUMN label       TEXT NOT NULL DEFAULT '',
    ADD COLUMN created_via TEXT NOT NULL DEFAULT 'magic_link';

ALTER TABLE api_keys_registry
    ADD CONSTRAINT api_keys_registry_created_via_valid
        CHECK (created_via IN ('magic_link', 'agent')),
    ADD CONSTRAINT api_keys_registry_label_len
        CHECK (char_length(label) <= 64);

-- Dropping this index is the point of the migration: it was the mechanism that
-- made a second active key impossible.
DROP INDEX IF EXISTS idx_api_keys_registry_one_active_per_identity;

-- Partial index serving both the INSERT-time cap count and per-identity key
-- listing. Narrower than idx_api_keys_registry_identity because it excludes
-- revoked rows, which grow without bound.
CREATE INDEX idx_api_keys_registry_active_by_identity
    ON api_keys_registry (identity_id, created_at DESC)
    WHERE status = 'active';

-- ── agent_grants ─────────────────────────────────────────────────────────────
-- One row per device-authorization grant. Lifecycle:
--   pending  → created by POST /auth/agent/device, awaiting a human decision
--   approved → a signed-in user approved it for identity_id; agent may redeem
--   denied   → the user explicitly refused
--   redeemed → the agent collected the key; the plaintext is never served again
--   expired  → TTL elapsed without redemption (also reachable from approved)
CREATE TABLE agent_grants (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- sha256 of the raw device code. Unique so a hash collision cannot make one
    -- grant redeemable by another's code.
    device_code_hash TEXT        NOT NULL,
    -- Short, human-typed code shown on the approval screen. Stored without the
    -- display hyphen; normalised to upper-case Crockford base32.
    user_code        TEXT        NOT NULL,
    -- Agent's self-reported name, shown verbatim on the approval screen. The
    -- frontend must render it as text, never as markup.
    client_name      TEXT        NOT NULL,
    platform         TEXT        NOT NULL DEFAULT '',
    -- Set only when a user approves. NULL while pending/denied/expired.
    identity_id      UUID        REFERENCES api_identities(id) ON DELETE CASCADE,
    status           TEXT        NOT NULL DEFAULT 'pending',
    expires_at       TIMESTAMPTZ NOT NULL,
    decided_at       TIMESTAMPTZ,
    redeemed_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT agent_grants_device_code_hash_unique   UNIQUE (device_code_hash),
    CONSTRAINT agent_grants_user_code_unique          UNIQUE (user_code),
    CONSTRAINT agent_grants_device_code_hash_nonempty
        CHECK (btrim(device_code_hash) <> ''),
    -- Crockford base32, 8 chars: no I, L, O or U, so a code read aloud or typed
    -- from a terminal cannot be transcribed ambiguously.
    CONSTRAINT agent_grants_user_code_format
        CHECK (user_code ~ '^[0-9A-HJKMNP-TV-Z]{8}$'),
    CONSTRAINT agent_grants_status_valid
        CHECK (status IN ('pending', 'approved', 'denied', 'redeemed', 'expired')),
    CONSTRAINT agent_grants_client_name_len
        CHECK (char_length(client_name) BETWEEN 1 AND 64),
    CONSTRAINT agent_grants_platform_len
        CHECK (char_length(platform) <= 32),
    -- An approved or redeemed grant must be bound to an identity; every other
    -- state must NOT be. The negative half matters: it is what makes it
    -- impossible for a pending or denied grant to carry the approver's account,
    -- so a denial cannot leak who was signed in, and no future code path can
    -- pre-commit an association before the user has actually decided.
    --
    -- Note for future writers: nothing sets status = 'expired' today (expiry is
    -- computed on read, and pruning deletes rows), so this CHECK has no live
    -- interaction with it. A future "expire approved-but-unredeemed grants"
    -- sweep must NULL identity_id in the same statement or it will violate this
    -- constraint — which is the intended forcing function, since an expired
    -- grant should not retain an account association.
    CONSTRAINT agent_grants_identity_required
        CHECK (
            (status IN ('approved', 'redeemed') AND identity_id IS NOT NULL) OR
            (status IN ('pending', 'denied', 'expired') AND identity_id IS NULL)
        ),
    -- Enforce consistent status/timestamp states so no code path can leave a
    -- grant half-decided (e.g. redeemed without a decision).
    CONSTRAINT agent_grants_state_consistent
        CHECK (
            (status = 'pending'  AND decided_at IS NULL     AND redeemed_at IS NULL) OR
            (status = 'expired'  AND redeemed_at IS NULL)                            OR
            (status = 'approved' AND decided_at IS NOT NULL AND redeemed_at IS NULL) OR
            (status = 'denied'   AND decided_at IS NOT NULL AND redeemed_at IS NULL) OR
            (status = 'redeemed' AND decided_at IS NOT NULL AND redeemed_at IS NOT NULL)
        )
);

-- Pruning: DELETE FROM agent_grants WHERE expires_at < NOW().
CREATE INDEX idx_agent_grants_expiry ON agent_grants (expires_at);
