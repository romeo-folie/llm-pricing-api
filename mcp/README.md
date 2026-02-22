# @llmrates/mcp

MCP server that gives AI agents real-time LLM pricing data — current prices, price history, change alerts, and model recommendations.

---

## Install

Run directly with npx (no install step required):

```bash
npx @llmrates/mcp
```

---

## Configuration

### Claude Code (`~/.claude/mcp.json`)

```json
{
  "mcpServers": {
    "llmrates": {
      "command": "npx",
      "args": ["@llmrates/mcp"],
      "env": {
        "LLMRATES_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

### Cursor (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "llmrates": {
      "command": "npx",
      "args": ["@llmrates/mcp"],
      "env": {
        "LLMRATES_API_KEY": "your-api-key-here"
      }
    }
  }
}
```

---

## Tools

| Tool | Description | Required params | Tier |
|------|-------------|-----------------|------|
| `get_cheapest_model` | Find the cheapest model matching your task requirements. Returns a ranked list with pricing and trust metadata. | `task` | Developer |
| `compare_models` | Compare up to 5 models side-by-side with current pricing and trust metadata. | `models` (array, 2–5 IDs) | Free |
| `get_price_history` | Retrieve the full pricing history for a specific model with timestamped records and source attribution. | `model_id` | Developer |
| `get_recent_changes` | Get recent LLM pricing changes across all providers, optionally filtered by provider or time range. | — | Free |
| `get_context_snapshot` | Get a ~2k-token pricing snapshot optimised for agent system prompts. Supports `json` and `markdown` output. | — | Developer |
| `subscribe_to_changes` | Register an HTTPS webhook URL to receive real-time notifications when prices change. | `url` | Pro |

### Tool details

#### `get_cheapest_model`

```json
{
  "task": "code generation",
  "min_context": 32000,
  "max_input_price": 5.0,
  "modality": "text"
}
```

#### `compare_models`

```json
{
  "models": ["openai/gpt-4o", "anthropic/claude-3-5-sonnet", "google/gemini-1.5-pro"]
}
```

#### `get_price_history`

```json
{
  "model_id": "openai/gpt-4o",
  "from": "2025-01-01T00:00:00Z",
  "to": "2025-06-01T00:00:00Z"
}
```

#### `get_recent_changes`

```json
{
  "since": "2025-06-01T00:00:00Z",
  "provider": "anthropic"
}
```

#### `get_context_snapshot`

```json
{
  "format": "markdown"
}
```

#### `subscribe_to_changes`

```json
{
  "url": "https://your-service.com/webhooks/llmrates",
  "events": ["price_change", "model_added"]
}
```

---

## Example output

`get_recent_changes` returns:

```json
{
  "data": [
    {
      "model_id": "anthropic/claude-3-5-sonnet",
      "provider": "anthropic",
      "changed_at": "2025-09-14T08:22:11Z",
      "field": "input_price_per_1m_tokens",
      "old_value": 3.00,
      "new_value": 2.50,
      "change_pct": -16.67,
      "confidence": "high",
      "sources": ["openrouter", "anthropic_docs"],
      "confirmed_at": "2025-09-14T08:30:00Z"
    },
    {
      "model_id": "openai/gpt-4o-mini",
      "provider": "openai",
      "changed_at": "2025-09-10T14:05:33Z",
      "field": "output_price_per_1m_tokens",
      "old_value": 0.60,
      "new_value": 0.40,
      "change_pct": -33.33,
      "confidence": "high",
      "sources": ["openrouter", "litellm", "openai_docs"],
      "confirmed_at": "2025-09-10T14:15:00Z"
    }
  ],
  "meta": {
    "total": 2,
    "age_hours": 0.5
  }
}
```

---

## Environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LLMRATES_API_KEY` | Yes | — | Your LLM Rates API key. Get one at [llmrates.com](https://llmrates.com). |
| `LLMRATES_API_URL` | No | `https://api.llmrates.com` | Override the API base URL (useful for self-hosted or staging deployments). |

---

## Tiers

| Tier | Price | Rate limit | Tools available |
|------|-------|------------|-----------------|
| Free | $0/month | 100 req/day | `compare_models`, `get_recent_changes` |
| Developer | $10/month | 10,000 req/day | All tools except `subscribe_to_changes` |
| Pro | $15/month | Unlimited | All tools including webhooks + SLA |

Tools that require a higher tier return a descriptive error message — no silent failures.

---

## Get your API key

[https://llmrates.com](https://llmrates.com)
