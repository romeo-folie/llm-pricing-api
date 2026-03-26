CREATE TABLE weight_preset_history (
  id            BIGSERIAL PRIMARY KEY,
  use_case      TEXT NOT NULL,
  priority      TEXT NOT NULL,
  weights       JSONB NOT NULL,
  source        TEXT NOT NULL DEFAULT 'hardcoded',
  effective_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON weight_preset_history (use_case, priority, effective_at DESC);
