-- Revert: remove the anthropic source row inserted/updated by the up migration.
-- Restores the post-000008 state where anthropic was absent from sources.
--
-- Dependent rows (prices, price_history, review_queue) must be removed first
-- because sources.id is referenced with RESTRICT (no CASCADE).  The subquery
-- approach is a no-op when no matching rows exist, so this is safe on a fresh
-- DB that never ran the scraper.
DELETE FROM review_queue
    WHERE source_a IN (SELECT id FROM sources WHERE name = 'anthropic')
       OR source_b IN (SELECT id FROM sources WHERE name = 'anthropic');
DELETE FROM price_history
    WHERE source_id IN (SELECT id FROM sources WHERE name = 'anthropic');
DELETE FROM prices
    WHERE source_id IN (SELECT id FROM sources WHERE name = 'anthropic');
DELETE FROM sources WHERE name = 'anthropic';
