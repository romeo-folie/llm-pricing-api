CREATE TABLE recommendation_feedback (
  id            BIGSERIAL PRIMARY KEY,
  session_id    TEXT NOT NULL,
  api_key_id    TEXT,
  use_case      TEXT NOT NULL,
  model_slug    TEXT NOT NULL REFERENCES models(slug),
  signal        SMALLINT NOT NULL CHECK (signal IN (-1, 1)),
  context       TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON recommendation_feedback (use_case, model_slug);
CREATE INDEX ON recommendation_feedback (created_at DESC);
