CREATE TABLE IF NOT EXISTS model_benchmark_scores (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id          INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    benchmark_id      UUID    NOT NULL REFERENCES benchmarks(id) ON DELETE CASCADE,
    raw_score         NUMERIC,
    normalized_score  NUMERIC CHECK (normalized_score IS NULL OR (normalized_score >= 0 AND normalized_score <= 100)),
    benchmark_version TEXT        NOT NULL DEFAULT 'latest',
    source_url        TEXT        NOT NULL,
    confidence        TEXT        NOT NULL CHECK (confidence IN ('high','medium','low')),
    evaluated_at      TIMESTAMPTZ NOT NULL,
    ingested_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (model_id, benchmark_id, benchmark_version)
);

CREATE INDEX IF NOT EXISTS idx_mbs_model_id     ON model_benchmark_scores(model_id);
CREATE INDEX IF NOT EXISTS idx_mbs_benchmark_id ON model_benchmark_scores(benchmark_id);
