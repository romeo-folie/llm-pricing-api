CREATE TABLE IF NOT EXISTS model_capability_scores (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id        INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    dimension       TEXT    NOT NULL CHECK (dimension IN (
                      'quality','coding','tool_use','agentic','finance','writing',
                      'video','reasoning','instruction','structured_output',
                      'preference','latency','cost_efficiency'
                    )),
    score           NUMERIC CHECK (score IS NULL OR (score >= 0 AND score <= 100)),
    confidence      TEXT    NOT NULL CHECK (confidence IN ('high','medium','low')),
    benchmark_count INT     NOT NULL DEFAULT 0,
    freshness       TEXT    NOT NULL DEFAULT 'fresh' CHECK (freshness IN ('fresh','stale')),
    computed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (model_id, dimension)
);

CREATE INDEX IF NOT EXISTS idx_mcs_model_id  ON model_capability_scores(model_id);
CREATE INDEX IF NOT EXISTS idx_mcs_dimension ON model_capability_scores(dimension);
