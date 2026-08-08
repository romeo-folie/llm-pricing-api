DROP INDEX IF EXISTS idx_mbs_active_evidence;

ALTER TABLE model_benchmark_scores
    DROP COLUMN IF EXISTS last_observed_at,
    DROP COLUMN IF EXISTS source_entry_name,
    DROP COLUMN IF EXISTS source_model_name;
