-- Revert: remove the openai source row inserted/updated by the up migration.
-- This restores the post-000008 state where openai was absent from sources.
--
-- Must delete FK-dependent rows first (prices → price_history → review_queue
-- all reference source_id with RESTRICT semantics). Cascade order:
--   1. review_queue (references prices)
--   2. price_history (references prices)
--   3. prices (references sources)
--   4. sources (the row itself)
DELETE FROM review_queue
WHERE price_id IN (SELECT id FROM prices WHERE source_id = (SELECT id FROM sources WHERE name = 'openai'));

DELETE FROM price_history
WHERE price_id IN (SELECT id FROM prices WHERE source_id = (SELECT id FROM sources WHERE name = 'openai'));

DELETE FROM prices
WHERE source_id = (SELECT id FROM sources WHERE name = 'openai');

DELETE FROM sources WHERE name = 'openai';
