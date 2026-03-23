-- Remove only the seeded benchmark scores inserted by this migration.
-- Rows inserted by the application have a different benchmark_version
-- and are left intact.
DELETE FROM model_benchmark_scores WHERE benchmark_version = 'seed-2024';
