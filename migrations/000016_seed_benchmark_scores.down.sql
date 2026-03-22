-- Remove all seeded benchmark scores. Rows inserted by the application
-- (without a source_url matching the seed pattern) are left intact.
DELETE FROM model_benchmark_scores WHERE source_url IS NOT NULL;
