-- Restore the Google source row removed by migration 000008.
-- The Gemini HTML scraper added in this branch emits diffs with
-- SourceName "google" and requires a matching sources row so that the
-- reconciler accepts them.
-- INSERT ... ON CONFLICT is idempotent: safe on both fresh and existing DBs.
INSERT INTO sources (name, url, type)
VALUES ('google', 'https://ai.google.dev/gemini-api/docs/pricing', 'scrape')
ON CONFLICT (name) DO UPDATE
    SET url  = EXCLUDED.url,
        type = EXCLUDED.type;
