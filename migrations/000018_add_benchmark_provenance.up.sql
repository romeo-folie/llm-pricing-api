ALTER TABLE model_benchmark_scores
    ADD COLUMN IF NOT EXISTS source_model_name TEXT,
    ADD COLUMN IF NOT EXISTS source_entry_name TEXT,
    ADD COLUMN IF NOT EXISTS last_observed_at TIMESTAMPTZ;

UPDATE model_benchmark_scores
SET last_observed_at = ingested_at
WHERE last_observed_at IS NULL;

ALTER TABLE model_benchmark_scores
    ALTER COLUMN last_observed_at SET DEFAULT NOW(),
    ALTER COLUMN last_observed_at SET NOT NULL;

-- Active evidence is the most recently evaluated row for each model and
-- benchmark. Producers provide last_observed_at from the source snapshot;
-- evidence-content fields provide deterministic final tie-breakers.
CREATE INDEX IF NOT EXISTS idx_mbs_active_evidence
    ON model_benchmark_scores
       (model_id, benchmark_id, last_observed_at DESC, evaluated_at DESC)
    WHERE normalized_score IS NOT NULL;
