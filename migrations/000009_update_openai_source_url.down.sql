-- Revert: remove the openai source row inserted/updated by the up migration.
-- This restores the post-000008 state where openai was removed from sources.
DELETE FROM sources WHERE name = 'openai';
