# internal/scraper

Data source scrapers.

## Purpose

Fetches LLM pricing data from external sources on a scheduled cadence. Each scraper normalises its source's data format into the common domain model for the reconciliation engine to process.

## Structure

```
internal/scraper/
  README.md    # This file
```

Currently a placeholder — scrapers will be added in Phase 1.

## Planned Scrapers

| Source | Frequency | Method |
|---|---|---|
| OpenRouter `/v1/models` | Every 6 hours | REST API call |
| LiteLLM model cost map | Daily | GitHub raw JSON fetch |
| Provider docs (OpenAI, Anthropic, Google, Mistral, Amazon) | Daily | HTML scrape / API |

## Dependencies (planned)

| Dependency | Role |
|---|---|
| `internal/models` | Common domain structs |
| `internal/reconciler` | Submits scraped data for reconciliation |
| `github.com/hibiken/asynq` | Scheduled via asynq cron tasks |
