-- Restore the OpenAI source row and set the current developer pricing URL.
-- Migration 000008 removed the 'openai' row; the scrapers added in this branch
-- require it to be present so reconciliation can accept their diffs.
-- Using INSERT ... ON CONFLICT so this is idempotent on both fresh and existing DBs:
--   fresh DB (post-000008): row is absent → INSERT creates it.
--   existing DB (pre-000008): row exists with old URL → UPDATE sets the canonical URL.
INSERT INTO sources (name, url, type)
VALUES ('openai', 'https://developers.openai.com/api/docs/pricing?latest-pricing=standard', 'scrape')
ON CONFLICT (name) DO UPDATE
    SET url  = EXCLUDED.url,
        type = EXCLUDED.type;
