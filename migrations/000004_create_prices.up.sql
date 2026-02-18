CREATE TABLE prices (
    id                    SERIAL PRIMARY KEY,
    model_id              INTEGER     NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    input_cost_per_token  NUMERIC(20, 10) NOT NULL,
    output_cost_per_token NUMERIC(20, 10) NOT NULL,
    source_id             INTEGER     NOT NULL REFERENCES sources (id),
    confirmed_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confidence            TEXT        NOT NULL DEFAULT 'medium' CHECK (confidence IN ('high', 'medium', 'low')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (model_id, source_id)
);

CREATE INDEX idx_prices_model_id  ON prices (model_id);
CREATE INDEX idx_prices_source_id ON prices (source_id);
