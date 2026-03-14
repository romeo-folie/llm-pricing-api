-- Revert: remove the openai source row inserted/updated by the up migration.
-- This restores the post-000008 state where openai was absent from sources.
--
-- Must delete FK-dependent rows first (sources.id is referenced with RESTRICT
-- by prices, price_history, and review_queue). Cascade order:
--   1. review_queue  — references sources via source_a / source_b
--   2. price_history — references sources via source_id
--   3. prices        — references sources via source_id
--   4. sources       — the row itself
DELETE FROM review_queue
WHERE source_a = (SELECT id FROM sources WHERE name = 'openai')
   OR source_b = (SELECT id FROM sources WHERE name = 'openai');

DELETE FROM price_history
WHERE source_id = (SELECT id FROM sources WHERE name = 'openai');

DELETE FROM prices
WHERE source_id = (SELECT id FROM sources WHERE name = 'openai');

DELETE FROM sources WHERE name = 'openai';
