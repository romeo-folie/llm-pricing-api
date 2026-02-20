-- Add HuggingFace Inference Providers as an active scraper source
INSERT INTO sources (name, url, type) VALUES
    ('huggingface_inference_providers', 'https://huggingface.co/api/models', 'api')
ON CONFLICT (name) DO NOTHING;

-- Remove stale provider-doc source rows whose scrapers were removed before any price data was written.
-- These rows are misleading (they imply live scraping) and must be cleaned up.
-- NOTE: prices.source_id, price_history.source_id, and review_queue.source_a/source_b all hold
-- REFERENCES sources(id) with default RESTRICT (no CASCADE). The DELETE is safe because no rows
-- in any of these tables reference these source IDs — their scrapers were removed before any
-- data was written or reconciled.
DELETE FROM sources WHERE name IN ('openai', 'anthropic', 'google', 'mistral', 'amazon');

-- Add nullable underlying_provider provenance column.
-- Only set for pass-through aggregators (e.g. OpenRouter, HuggingFace) where the actual
-- infrastructure is provided by a third party (Together AI, Replicate, etc.).
-- NOTE: ADD COLUMN on a TimescaleDB hypertable propagates to all chunks. For a nullable column
-- with no DEFAULT this is a metadata-only operation per chunk (no table rewrite), but lock
-- acquisition scales with chunk count — run during low-traffic windows as the dataset grows.
ALTER TABLE prices        ADD COLUMN IF NOT EXISTS underlying_provider TEXT;
ALTER TABLE price_history ADD COLUMN IF NOT EXISTS underlying_provider TEXT;
