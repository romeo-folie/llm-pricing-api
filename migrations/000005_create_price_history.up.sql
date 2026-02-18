CREATE TABLE price_history (
    model_id              INTEGER        NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    input_cost_per_token  NUMERIC(20, 10) NOT NULL,
    output_cost_per_token NUMERIC(20, 10) NOT NULL,
    source_id             INTEGER        NOT NULL REFERENCES sources (id),
    confirmed_at          TIMESTAMPTZ    NOT NULL,
    recorded_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

-- Convert to a TimescaleDB hypertable partitioned by recorded_at.
-- chunk_time_interval of 7 days is appropriate for a pricing dataset
-- that changes infrequently but needs fast range scans.
SELECT create_hypertable('price_history', 'recorded_at', chunk_time_interval => INTERVAL '7 days');

CREATE INDEX idx_price_history_model_recorded ON price_history (model_id, recorded_at DESC);
CREATE INDEX idx_price_history_source         ON price_history (source_id);

-- Prevent duplicate writes from scrapers re-submitting the same confirmed event.
CREATE UNIQUE INDEX idx_price_history_dedup
    ON price_history (model_id, source_id, confirmed_at, recorded_at);
