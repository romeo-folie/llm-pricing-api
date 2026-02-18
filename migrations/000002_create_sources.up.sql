CREATE TABLE sources (
    id         SERIAL PRIMARY KEY,
    name       TEXT        NOT NULL UNIQUE,
    url        TEXT        NOT NULL,
    type       TEXT        NOT NULL CHECK (type IN ('api', 'github', 'scrape')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO sources (name, url, type) VALUES
    ('openrouter', 'https://openrouter.ai/api/v1/models', 'api'),
    ('litellm',    'https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json', 'github'),
    ('openai',     'https://openai.com/api/pricing', 'scrape'),
    ('anthropic',  'https://www.anthropic.com/pricing', 'scrape'),
    ('google',     'https://ai.google.dev/pricing', 'scrape'),
    ('mistral',    'https://mistral.ai/technology/#pricing', 'scrape'),
    ('amazon',     'https://aws.amazon.com/bedrock/pricing', 'scrape');
