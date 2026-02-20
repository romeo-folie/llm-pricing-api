-- Add HuggingFace Inference Providers as an active scraper source
INSERT INTO sources (name, url, type) VALUES
    ('huggingface_inference_providers', 'https://huggingface.co/api/models', 'api');

-- Remove stale provider-doc source rows whose scrapers were removed before any price data was written.
-- These rows are misleading (they imply live scraping) and must be cleaned up.
-- NOTE: prices.source_id and price_history.source_id have no ON DELETE CASCADE — the DELETE is safe
-- because no price rows were ever written for these sources.
DELETE FROM sources WHERE name IN ('openai', 'anthropic', 'google', 'mistral', 'amazon');

-- Add nullable underlying_provider provenance column.
-- Only set for pass-through aggregators (e.g. OpenRouter, HuggingFace) where the actual
-- infrastructure is provided by a third party (Together AI, Replicate, etc.).
ALTER TABLE prices        ADD COLUMN underlying_provider TEXT;
ALTER TABLE price_history ADD COLUMN underlying_provider TEXT;
