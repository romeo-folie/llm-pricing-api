CREATE TABLE billing_subscriptions (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ls_subscription_id TEXT        NOT NULL UNIQUE,
    ls_customer_email  TEXT        NOT NULL,
    tier               TEXT        NOT NULL CHECK (tier IN ('free', 'developer', 'pro')),
    unkey_key_id       TEXT        NOT NULL,
    status             TEXT        NOT NULL DEFAULT 'active',
    revoke_job_id      TEXT,
    renews_at          TIMESTAMPTZ,
    cancelled_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE webhook_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id     TEXT        NOT NULL UNIQUE,
    event_type   TEXT        NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
