-- Update the OpenAI source URL to the current developer pricing page.
-- The old URL (https://openai.com/api/pricing) now returns HTTP 403.
-- The canonical pricing data is served at developers.openai.com.
UPDATE sources
SET url = 'https://developers.openai.com/api/docs/pricing?latest-pricing=standard'
WHERE name = 'openai';
