-- Revert: remove the google source row inserted/updated by the up migration.
-- Restores the post-000008 state where google was removed from sources.
DELETE FROM sources WHERE name = 'google';
