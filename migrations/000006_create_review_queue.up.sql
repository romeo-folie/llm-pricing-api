CREATE TABLE review_queue (
    id          SERIAL PRIMARY KEY,
    model_id    INTEGER        NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    source_a    INTEGER        NOT NULL REFERENCES sources (id),
    source_b    INTEGER        NOT NULL REFERENCES sources (id),
    field       TEXT           NOT NULL CHECK (field IN ('input_cost_per_token', 'output_cost_per_token')),
    value_a     NUMERIC(20, 10) NOT NULL,
    value_b     NUMERIC(20, 10) NOT NULL,
    delta_pct   NUMERIC(10, 4)  NOT NULL,
    status      TEXT           NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'resolved', 'overridden')),
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_review_queue_model_status ON review_queue (model_id, status);
CREATE INDEX idx_review_queue_status       ON review_queue (status) WHERE status = 'pending';

-- Only one active (pending) dispute per model+field at a time.
-- Prevents the reconciler from flooding the queue before a reviewer acts.
CREATE UNIQUE INDEX idx_review_queue_active
    ON review_queue (model_id, field)
    WHERE status = 'pending';
