-- Revert OpenAI source URL to the original value from 000002_create_sources.
UPDATE sources
SET url = 'https://openai.com/api/pricing'
WHERE name = 'openai';
