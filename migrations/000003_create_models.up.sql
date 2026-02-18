CREATE TABLE models (
    id             SERIAL PRIMARY KEY,
    provider       TEXT        NOT NULL,
    name           TEXT        NOT NULL,
    slug           TEXT        NOT NULL UNIQUE,
    modality       TEXT        NOT NULL CHECK (modality IN ('text', 'multimodal', 'image', 'audio', 'embedding')),
    context_window INTEGER,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_models_provider ON models (provider);
CREATE INDEX idx_models_modality  ON models (modality);

-- Auto-update updated_at on any row modification so scrapers don't have to remember to set it.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_models_updated_at
    BEFORE UPDATE ON models
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
