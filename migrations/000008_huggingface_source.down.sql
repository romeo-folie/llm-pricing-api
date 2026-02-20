ALTER TABLE price_history DROP COLUMN IF EXISTS underlying_provider;
ALTER TABLE prices        DROP COLUMN IF EXISTS underlying_provider;

DELETE FROM sources WHERE name = 'huggingface_inference_providers';

-- Restore stale rows so the down migration is reversible.
-- NOTE: Restored rows will receive new serial IDs from the sequence, not their original IDs.
-- This is acceptable because no rows in prices, price_history, or review_queue ever referenced
-- these source IDs — the stale scrapers were removed before any data was written.
INSERT INTO sources (name, url, type) VALUES
    ('openai',    'https://openai.com/api/pricing',            'scrape'),
    ('anthropic', 'https://www.anthropic.com/pricing',         'scrape'),
    ('google',    'https://ai.google.dev/pricing',             'scrape'),
    ('mistral',   'https://mistral.ai/technology/#pricing',    'scrape'),
    ('amazon',    'https://aws.amazon.com/bedrock/pricing',    'scrape')
ON CONFLICT (name) DO NOTHING;
