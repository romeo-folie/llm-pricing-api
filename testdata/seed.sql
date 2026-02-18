-- seed.sql — deterministic test data for integration tests.
-- This file is idempotent: TRUNCATE + re-insert on every test run.
-- Tables are truncated in dependency order (child before parent).

TRUNCATE TABLE webhooks           RESTART IDENTITY CASCADE;
TRUNCATE TABLE price_history      RESTART IDENTITY CASCADE;
TRUNCATE TABLE prices             RESTART IDENTITY CASCADE;
TRUNCATE TABLE models             RESTART IDENTITY CASCADE;
-- Sources are seeded by the migration; keep them stable and reset identity.
-- We do NOT truncate sources so the migration-seeded rows stay intact.
-- Instead we delete any test-only rows we may have added in prior runs.
DELETE FROM sources WHERE name IN ('openrouter-test', 'litellm-test');

-- -----------------------------------------------------------------------
-- Sources: reuse the migration-seeded rows by name (IDs may vary).
-- We reference them by name in price_history INSERTs below.
-- -----------------------------------------------------------------------

-- -----------------------------------------------------------------------
-- Models: 10 rows across 3 providers and 3+ modalities
-- Providers: openai (4), anthropic (3), google (3)
-- Modalities: text (6), multimodal (2), embedding (2)
-- Context ≥ 100k: models 1 (128k), 3 (128k), 5 (200k), 8 (131k)
-- -----------------------------------------------------------------------

INSERT INTO models (id, provider, name, slug, modality, context_window) VALUES
  (1,  'openai',    'GPT-4o',                   'openai/gpt-4o',                    'multimodal', 128000),
  (2,  'openai',    'GPT-4o Mini',               'openai/gpt-4o-mini',               'text',        16384),
  (3,  'openai',    'GPT-4 Turbo',               'openai/gpt-4-turbo',               'text',       128000),
  (4,  'openai',    'text-embedding-3-large',    'openai/text-embedding-3-large',    'embedding',    8192),
  (5,  'anthropic', 'Claude 3.5 Sonnet',         'anthropic/claude-3-5-sonnet',      'text',       200000),
  (6,  'anthropic', 'Claude 3 Haiku',            'anthropic/claude-3-haiku',         'text',        48000),
  (7,  'anthropic', 'Claude 3.5 Haiku',          'anthropic/claude-3-5-haiku',       'multimodal',  48000),
  (8,  'google',    'Gemini 1.5 Pro',            'google/gemini-1-5-pro',            'text',       131072),
  (9,  'google',    'Gemini 1.5 Flash',          'google/gemini-1-5-flash',          'text',        16384),
  (10, 'google',    'text-embedding-004',        'google/text-embedding-004',        'embedding',    2048)
ON CONFLICT (slug) DO UPDATE SET
  provider       = EXCLUDED.provider,
  name           = EXCLUDED.name,
  modality       = EXCLUDED.modality,
  context_window = EXCLUDED.context_window,
  updated_at     = NOW();

-- Reset the models sequence past our highest inserted ID so subsequent inserts
-- (e.g. from webhook-related tests) don't collide.
SELECT setval('models_id_seq', 100, false);

-- -----------------------------------------------------------------------
-- Prices (latest snapshot for each model — used by ListModels / GetModel)
-- -----------------------------------------------------------------------

INSERT INTO prices (model_id, input_cost_per_token, output_cost_per_token, source_id, confirmed_at, confidence)
SELECT
  m.id,
  p.input_cost,
  p.output_cost,
  s.id,
  NOW() - INTERVAL '1 hour',
  'high'
FROM (VALUES
  ('openai/gpt-4o',                 0.000002500, 0.000010000, 'openrouter'),
  ('openai/gpt-4o-mini',            0.000000150, 0.000000600, 'openrouter'),
  ('openai/gpt-4-turbo',            0.000010000, 0.000030000, 'openrouter'),
  ('openai/text-embedding-3-large', 0.000000130, 0.000000000, 'openrouter'),
  ('anthropic/claude-3-5-sonnet',   0.000003000, 0.000015000, 'litellm'),
  ('anthropic/claude-3-haiku',      0.000000250, 0.000001250, 'litellm'),
  ('anthropic/claude-3-5-haiku',    0.000000800, 0.000004000, 'litellm'),
  ('google/gemini-1-5-pro',         0.000003500, 0.000010500, 'openrouter'),
  ('google/gemini-1-5-flash',       0.000000075, 0.000000300, 'openrouter'),
  ('google/text-embedding-004',     0.000000000, 0.000000000, 'openrouter')
) AS p(slug, input_cost, output_cost, source_name)
JOIN models m   ON m.slug = p.slug
JOIN sources s  ON s.name = p.source_name
ON CONFLICT (model_id, source_id) DO UPDATE SET
  input_cost_per_token  = EXCLUDED.input_cost_per_token,
  output_cost_per_token = EXCLUDED.output_cost_per_token,
  confirmed_at          = EXCLUDED.confirmed_at,
  confidence            = EXCLUDED.confidence;

-- -----------------------------------------------------------------------
-- Price history: 3+ models with 2+ distinct source IDs each.
-- Models with history: openai/gpt-4o (id=1), anthropic/claude-3-5-sonnet (id=5),
-- google/gemini-1-5-pro (id=8).
-- Sources used: openrouter (id varies) and litellm (id varies).
-- Each block inserts two rows per model per source at different timestamps to
-- produce real change events that ListChanges can detect.
-- -----------------------------------------------------------------------

INSERT INTO price_history
  (model_id, input_cost_per_token, output_cost_per_token, source_id, confirmed_at, recorded_at)
SELECT
  m.id,
  ph.input_cost,
  ph.output_cost,
  s.id,
  ph.confirmed_at,
  ph.confirmed_at + INTERVAL '30 seconds'
FROM (VALUES
  -- openai/gpt-4o — openrouter, two price points (price changed)
  ('openai/gpt-4o', 0.000002000, 0.000008000, 'openrouter', NOW() - INTERVAL '48 hours'),
  ('openai/gpt-4o', 0.000002500, 0.000010000, 'openrouter', NOW() - INTERVAL '1 hour'),
  -- openai/gpt-4o — litellm, two price points
  ('openai/gpt-4o', 0.000002000, 0.000008000, 'litellm',    NOW() - INTERVAL '47 hours'),
  ('openai/gpt-4o', 0.000002500, 0.000010000, 'litellm',    NOW() - INTERVAL '2 hours'),

  -- anthropic/claude-3-5-sonnet — openrouter, two price points
  ('anthropic/claude-3-5-sonnet', 0.000002500, 0.000012500, 'openrouter', NOW() - INTERVAL '72 hours'),
  ('anthropic/claude-3-5-sonnet', 0.000003000, 0.000015000, 'openrouter', NOW() - INTERVAL '3 hours'),
  -- anthropic/claude-3-5-sonnet — litellm, two price points
  ('anthropic/claude-3-5-sonnet', 0.000002500, 0.000012500, 'litellm',    NOW() - INTERVAL '71 hours'),
  ('anthropic/claude-3-5-sonnet', 0.000003000, 0.000015000, 'litellm',    NOW() - INTERVAL '4 hours'),

  -- google/gemini-1-5-pro — openrouter, two price points
  ('google/gemini-1-5-pro', 0.000002500, 0.000007500, 'openrouter', NOW() - INTERVAL '96 hours'),
  ('google/gemini-1-5-pro', 0.000003500, 0.000010500, 'openrouter', NOW() - INTERVAL '5 hours'),
  -- google/gemini-1-5-pro — litellm, two price points
  ('google/gemini-1-5-pro', 0.000002500, 0.000007500, 'litellm',    NOW() - INTERVAL '95 hours'),
  ('google/gemini-1-5-pro', 0.000003500, 0.000010500, 'litellm',    NOW() - INTERVAL '6 hours')
) AS ph(slug, input_cost, output_cost, source_name, confirmed_at)
JOIN models  m ON m.slug  = ph.slug
JOIN sources s ON s.name  = ph.source_name
ON CONFLICT (model_id, source_id, confirmed_at, recorded_at) DO NOTHING;
