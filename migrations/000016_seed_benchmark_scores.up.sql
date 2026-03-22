-- Seed benchmark scores for models that exist in the models table.
-- Uses slug-based lookups so this works regardless of auto-generated IDs.
-- All scores are from publicly available sources; NULL where data is unavailable.
-- ON CONFLICT DO NOTHING makes this idempotent.

-- ============================================================
-- Helper CTE: resolve benchmark names → UUIDs once.
-- ============================================================
WITH b AS (
    SELECT id, name FROM benchmarks
)

-- ============================================================
-- GPT-4o  (slug: openai/gpt-4o)
-- Sources: OpenAI system card, Chatbot Arena, official leaderboards
-- ============================================================
INSERT INTO model_benchmark_scores
    (model_id, benchmark_id, raw_score, normalized_score, benchmark_version, source_url, confidence, evaluated_at)

-- MMLU-Pro
SELECT m.id, b.id, 72.6, 72.6, 'seed-2024',openai.com/index/hello-gpt-4o/', 'medium', '2024-05-13'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'openai/gpt-4o'
UNION ALL
-- GPQA Diamond
SELECT m.id, b.id, 53.6, 53.6, 'seed-2024',openai.com/index/hello-gpt-4o/', 'medium', '2024-05-13'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'GPQA Diamond' AND m.slug = 'openai/gpt-4o'
UNION ALL
-- IFEval
SELECT m.id, b.id, 84.9, 84.9, 'seed-2024',openai.com/index/hello-gpt-4o/', 'medium', '2024-05-13'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'IFEval' AND m.slug = 'openai/gpt-4o'
UNION ALL
-- MATH-500
SELECT m.id, b.id, 76.6, 76.6, 'seed-2024',openai.com/index/hello-gpt-4o/', 'medium', '2024-05-13'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MATH-500' AND m.slug = 'openai/gpt-4o'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1285, 85.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'openai/gpt-4o'

UNION ALL

-- ============================================================
-- GPT-4o Mini  (slug: openai/gpt-4o-mini)
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 63.2, 63.2, 'seed-2024',openai.com/index/gpt-4o-mini-advancing-cost-efficient-intelligence/', 'medium', '2024-07-18'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'openai/gpt-4o-mini'
UNION ALL
-- MATH-500
SELECT m.id, b.id, 70.2, 70.2, 'seed-2024',openai.com/index/gpt-4o-mini-advancing-cost-efficient-intelligence/', 'medium', '2024-07-18'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MATH-500' AND m.slug = 'openai/gpt-4o-mini'
UNION ALL
-- IFEval
SELECT m.id, b.id, 80.0, 80.0, 'seed-2024',openai.com/index/gpt-4o-mini-advancing-cost-efficient-intelligence/', 'medium', '2024-07-18'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'IFEval' AND m.slug = 'openai/gpt-4o-mini'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1216, 78.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'openai/gpt-4o-mini'

UNION ALL

-- ============================================================
-- GPT-4 Turbo  (slug: openai/gpt-4-turbo)
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 64.1, 64.1, 'seed-2024',arxiv.org/abs/2406.01574', 'high', '2024-06-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'openai/gpt-4-turbo'
UNION ALL
-- GPQA Diamond
SELECT m.id, b.id, 49.3, 49.3, 'seed-2024',arxiv.org/abs/2311.12022', 'high', '2024-06-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'GPQA Diamond' AND m.slug = 'openai/gpt-4-turbo'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1260, 83.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'openai/gpt-4-turbo'

UNION ALL

-- ============================================================
-- Claude 3.5 Sonnet  (slug: anthropic/claude-3-5-sonnet)
-- Sources: Anthropic model card
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 78.0, 78.0, 'seed-2024',www.anthropic.com/news/claude-3-5-sonnet', 'medium', '2024-06-20'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'anthropic/claude-3-5-sonnet'
UNION ALL
-- GPQA Diamond
SELECT m.id, b.id, 65.0, 65.0, 'seed-2024',www.anthropic.com/news/claude-3-5-sonnet', 'medium', '2024-06-20'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'GPQA Diamond' AND m.slug = 'anthropic/claude-3-5-sonnet'
UNION ALL
-- MATH-500
SELECT m.id, b.id, 78.3, 78.3, 'seed-2024',www.anthropic.com/news/claude-3-5-sonnet', 'medium', '2024-06-20'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MATH-500' AND m.slug = 'anthropic/claude-3-5-sonnet'
UNION ALL
-- IFEval
SELECT m.id, b.id, 88.0, 88.0, 'seed-2024',www.anthropic.com/news/claude-3-5-sonnet', 'medium', '2024-06-20'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'IFEval' AND m.slug = 'anthropic/claude-3-5-sonnet'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1271, 84.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'anthropic/claude-3-5-sonnet'

UNION ALL

-- ============================================================
-- Claude 3 Haiku  (slug: anthropic/claude-3-haiku)
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 52.9, 52.9, 'seed-2024',arxiv.org/abs/2406.01574', 'high', '2024-06-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'anthropic/claude-3-haiku'
UNION ALL
-- IFEval
SELECT m.id, b.id, 75.4, 75.4, 'seed-2024',www.anthropic.com/news/claude-3-family', 'medium', '2024-03-04'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'IFEval' AND m.slug = 'anthropic/claude-3-haiku'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1178, 73.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'anthropic/claude-3-haiku'

UNION ALL

-- ============================================================
-- Claude 3.5 Haiku  (slug: anthropic/claude-3-5-haiku)
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 65.0, 65.0, 'seed-2024',www.anthropic.com/news/3-5-models-and-computer-use', 'medium', '2024-10-22'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'anthropic/claude-3-5-haiku'
UNION ALL
-- MATH-500
SELECT m.id, b.id, 69.6, 69.6, 'seed-2024',www.anthropic.com/news/3-5-models-and-computer-use', 'medium', '2024-10-22'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MATH-500' AND m.slug = 'anthropic/claude-3-5-haiku'
UNION ALL
-- IFEval
SELECT m.id, b.id, 82.0, 82.0, 'seed-2024',www.anthropic.com/news/3-5-models-and-computer-use', 'medium', '2024-10-22'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'IFEval' AND m.slug = 'anthropic/claude-3-5-haiku'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1230, 80.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'anthropic/claude-3-5-haiku'

UNION ALL

-- ============================================================
-- Gemini 1.5 Pro  (slug: google/gemini-1-5-pro)
-- Sources: Google DeepMind tech report
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 69.0, 69.0, 'seed-2024',arxiv.org/abs/2406.01574', 'high', '2024-06-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'google/gemini-1-5-pro'
UNION ALL
-- MATH-500
SELECT m.id, b.id, 67.7, 67.7, 'seed-2024',storage.googleapis.com/deepmind-media/gemini/gemini_v1_5_report.pdf', 'medium', '2024-05-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MATH-500' AND m.slug = 'google/gemini-1-5-pro'
UNION ALL
-- Video-MME
SELECT m.id, b.id, 81.3, 81.3, 'seed-2024',storage.googleapis.com/deepmind-media/gemini/gemini_v1_5_report.pdf', 'medium', '2024-05-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Video-MME' AND m.slug = 'google/gemini-1-5-pro'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1264, 83.5, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'google/gemini-1-5-pro'

UNION ALL

-- ============================================================
-- Gemini 1.5 Flash  (slug: google/gemini-1-5-flash)
-- ============================================================
-- MMLU-Pro
SELECT m.id, b.id, 59.1, 59.1, 'seed-2024',arxiv.org/abs/2406.01574', 'high', '2024-06-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MMLU-Pro' AND m.slug = 'google/gemini-1-5-flash'
UNION ALL
-- MATH-500
SELECT m.id, b.id, 54.9, 54.9, 'seed-2024',storage.googleapis.com/deepmind-media/gemini/gemini_v1_5_report.pdf', 'medium', '2024-05-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'MATH-500' AND m.slug = 'google/gemini-1-5-flash'
UNION ALL
-- Chatbot Arena
SELECT m.id, b.id, 1227, 79.0, 'seed-2024',lmarena.ai/?leaderboard', 'high', '2024-12-01'::TIMESTAMPTZ
FROM b, models m WHERE b.name = 'Chatbot Arena' AND m.slug = 'google/gemini-1-5-flash'

ON CONFLICT (model_id, benchmark_id, benchmark_version) DO NOTHING;
