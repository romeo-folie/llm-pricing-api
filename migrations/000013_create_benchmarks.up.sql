CREATE TABLE IF NOT EXISTS benchmarks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT UNIQUE NOT NULL,
    dimension   TEXT NOT NULL CHECK (dimension IN (
                  'quality','coding','tool_use','agentic','finance','writing',
                  'video','reasoning','instruction','structured_output',
                  'preference','latency','cost_efficiency'
                )),
    source_url  TEXT,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the 12 core benchmarks.
INSERT INTO benchmarks (name, dimension, source_url, description) VALUES
  ('MMLU-Pro',           'quality',           'https://huggingface.co/datasets/TIGER-Lab/MMLU-Pro',       'Massive Multitask Language Understanding Pro — harder MMLU variant'),
  ('GPQA Diamond',       'reasoning',         'https://arxiv.org/abs/2311.12022',                         'Graduate-level science questions from domain experts'),
  ('BFCL V3',            'tool_use',          'https://gorilla.cs.berkeley.edu/leaderboard.html',         'Berkeley Function Calling Leaderboard v3'),
  ('SWE-bench Verified', 'coding',            'https://www.swebench.com/',                                'Real GitHub issue resolution benchmark'),
  ('LiveCodeBench',      'coding',            'https://livecodebench.github.io/',                         'Contamination-free coding benchmark from live contests'),
  ('CFA',                'finance',           'https://www.cfainstitute.org/',                             'CFA exam questions assessing financial reasoning'),
  ('WritingBench',       'writing',           'https://writingbench.github.io/',                          'Comprehensive writing quality evaluation'),
  ('Video-MME',          'video',             'https://video-mme.github.io/',                             'Multi-modal video understanding evaluation'),
  ('MATH-500',           'reasoning',         'https://huggingface.co/datasets/lighteval/MATH',           'Competition math problems — MATH dataset 500-problem subset'),
  ('AIME 2025',          'reasoning',         'https://artofproblemsolving.com/wiki/index.php/2025_AIME', 'American Invitational Mathematics Examination 2025'),
  ('IFEval',             'instruction',       'https://arxiv.org/abs/2311.07911',                         'Instruction following evaluation — verifiable constraints'),
  ('Chatbot Arena',      'preference',        'https://lmsys.org/blog/2023-05-25-chatbot-arena/',         'Human preference ELO ratings from blind pairwise comparisons')
ON CONFLICT (name) DO NOTHING;
