-- Restore the Anthropic source row removed by migration 000008.
-- The Anthropic HTML scraper added in this branch requires a sources row
-- to exist so that reconciler diffs are accepted.
-- INSERT ... ON CONFLICT is idempotent: safe on both fresh and existing DBs.
INSERT INTO sources (name, url, type)
VALUES ('anthropic', 'https://platform.claude.com/docs/en/about-claude/pricing', 'scrape')
ON CONFLICT (name) DO UPDATE
    SET url  = EXCLUDED.url,
        type = EXCLUDED.type;
