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

The `env` block can be omitted entirely. The server then starts unauthenticated and the
`authenticate` tool walks the user through approving a key, which is stored locally for later runs.

---

## Tools

| Tool | Description | Required params | Access |
|------|-------------|-----------------|--------|
| `authenticate` | Get an API key for the user, or replace a missing/revoked one. Returns a code and a URL for them to approve, then stores the key locally. | — | None |
| `get_cheapest_model` | Find the cheapest model matching your task requirements. Returns a ranked list with pricing and trust metadata. | `task` | API key |
| `compare_models` | Compare up to 5 models side-by-side with current pricing and trust metadata. | `models` (array, 2–5 IDs) | API key |
| `get_price_history` | Retrieve the full pricing history for a specific model with timestamped records and source attribution. | `model_id` | API key |
| `get_recent_changes` | Get recent LLM pricing changes across all providers, optionally filtered by provider or time range. | — | API key |
| `get_context_snapshot` | Get a ~2k-token pricing snapshot optimised for agent system prompts. Supports `json` and `markdown` output. | — | API key |
| `subscribe_to_changes` | Register an HTTPS webhook URL to receive real-time notifications when prices change. | `url` | API key |

### Tool details

#### `authenticate`

Call this when a tool reports that no API key is configured, or when the API rejects the current
key. It is a two-phase flow, because the URL has to reach the user before they can approve, and a
tool result is not delivered until the call returns:

```json
{ "client_name": "Claude Code", "wait_seconds": 90 }
```

1. **First call** (no pending request) starts an authorization and returns a short code plus a URL.
   Show both to the user and ask them to open the URL and approve. No arguments are required;
   `client_name` is how this client is identified on their approval screen.
2. **Second call** collects the key, which is then stored locally so every other tool works. It
   waits up to `wait_seconds` (default 90, max 300) for the approval, so if the user is quick the
   flow completes in two calls.

The key is delivered straight to this process. It never has to be copied, pasted, or shown in a
chat, and the user's only action is a decision.

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
| `LLMRATES_API_KEY` | No | — | Your LLM Rates API key. Optional: when unset, the server starts anyway and the `authenticate` tool obtains one. |
| `LLMRATES_API_URL` | No | `https://api.llmrates.live` | Override the API base URL (useful for self-hosted or staging deployments). |
| `LLMRATES_CONFIG_DIR` | No | `~/.llmrates` | Where the credential file is read and written. |
| `LLMRATES_CLIENT_NAME` | No | `MCP client` | Default name shown to the user on the approval screen, overridden by the `client_name` argument. |

### Stored credentials

With no `LLMRATES_API_KEY`, the server starts in an unauthenticated state: every tool returns a
message telling the agent to call `authenticate`. Previously the server exited, which made that
tool unreachable and left the user with nothing to do but edit config by hand.

A key obtained through `authenticate` is written to:

```
~/.llmrates/credentials.json     # mode 0600, directory 0700
```

The containing directory must not be group- or world-accessible. If it is, the write is **refused**
with an error naming the fix (`chmod 700 ~/.llmrates`) rather than silently tightening a directory
you may have widened on purpose. The key is still installed for the current session in that case, so
an approval is never wasted, but it will not survive a restart until the directory is restricted.

The write is atomic (temp file plus `rename`), so a crash cannot leave a truncated file that
would read as "no key" and silently re-trigger the flow. `LLMRATES_API_KEY` still takes precedence
over the stored key, so an operator can override without deleting anything.

An in-flight authorization is kept in `<configDir>/pending-grant.json` and removed once the key is
collected, denied, or expired.

---

## Access

**The API is free.** Every tool works with any valid API key — there is no paid plan, no per-tool
entitlement, and no meaningful daily request cap. There are no tiers; an earlier version of this
file described Free/Developer/Pro plans that no longer exist.

`subscribe_to_changes` only accepts `https` URLs, and rejects any URL resolving to a private or
loopback address. Each API key may hold at most **5 active webhooks**; a 6th returns a conflict
error. Unsubscribing frees a slot. All failures return a descriptive error — no silent failures.

An account may hold up to **5 active API keys**, one per agent by default, so each can be revoked
independently. Keys are labelled with the client name that requested them.

---

## Get your API key

Ask the agent to call the `authenticate` tool, or visit
[https://llmrates.live/signup/free](https://llmrates.live/signup/free) to do it yourself in a
browser. Either way the API is free.
