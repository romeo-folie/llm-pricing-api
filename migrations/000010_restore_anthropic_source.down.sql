-- Revert: remove the anthropic source row inserted/updated by the up migration.
-- Restores the post-000008 state where anthropic was removed from sources.
DELETE FROM sources WHERE name = 'anthropic';
