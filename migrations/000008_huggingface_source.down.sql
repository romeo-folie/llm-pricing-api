ALTER TABLE price_history DROP COLUMN IF EXISTS underlying_provider;
ALTER TABLE prices        DROP COLUMN IF EXISTS underlying_provider;

DELETE FROM sources WHERE name = 'huggingface_inference_providers';

-- Restore stale rows so the down migration is fully reversible
INSERT INTO sources (name, url, type) VALUES
    ('openai',    'https://openai.com/api/pricing',            'scrape'),
    ('anthropic', 'https://www.anthropic.com/pricing',         'scrape'),
    ('google',    'https://ai.google.dev/pricing',             'scrape'),
    ('mistral',   'https://mistral.ai/technology/#pricing',    'scrape'),
    ('amazon',    'https://aws.amazon.com/bedrock/pricing',    'scrape');
