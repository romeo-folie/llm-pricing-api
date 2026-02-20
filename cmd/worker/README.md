# cmd/worker

Background job worker entrypoint for the LLM Pricing Platform.

## Purpose

Runs an asynq worker server that processes asynchronous scraper tasks from the Redis-backed job queue. On startup it connects to PostgreSQL, wires the scraper task handlers through the diff and reconciliation pipeline, and starts a cron scheduler that enqueues scrape jobs at the configured intervals. The worker is the sole process responsible for keeping pricing data fresh. It also runs a minimal HTTP health server on `APP_PORT` (default 8080) so that Railway's health check can verify the service is alive.

## Structure

```
cmd/worker/
  main.go      # Worker entrypoint — DB, Redis, asynq server, handler registration, cron scheduler
  README.md    # This file
```

## Key Components

- **`main()`** — Loads `.env` via godotenv, reads config, opens a PostgreSQL connection pool with 5-attempt retry (matching `cmd/api`), creates an asynq server (concurrency 10), registers scraper handlers on the `ServeMux`, wires a cron scheduler with per-source intervals, starts a minimal HTTP health server on `APP_PORT`, and blocks until `SIGINT`/`SIGTERM` triggers graceful shutdown of the health server, asynq server, and scheduler.
- **`GET /health`** — Pings both PostgreSQL and Redis. Returns `{"status":"ok","db":"ok","redis":"ok"}` (200) when healthy, or `{"status":"degraded"}` (503) when either dependency is unreachable. Used by Railway's health check to verify the worker is running.

## Tasks and Cron Schedule

| Task constant | Task type string | Scraper | Schedule |
|---|---|---|---|
| `TaskOpenRouterScrape` | `scrape:openrouter` | OpenRouter API | Every 6 hours |
| `TaskLiteLLMScrape` | `scrape:litellm` | LiteLLM GitHub JSON | Every 24 hours |
| `TaskOpenAIScrape` | `scrape:openai` | OpenAI pricing page | Every 24 hours |
| `TaskAnthropicScrape` | `scrape:anthropic` | Anthropic pricing page | Every 24 hours |
| `TaskGoogleScrape` | `scrape:google` | Google pricing page | Every 24 hours |
| `TaskMistralScrape` | `scrape:mistral` | Mistral pricing page | Every 24 hours |
| `TaskAmazonScrape` | `scrape:amazon` | Amazon Bedrock pricing page | Every 24 hours |

## Pipeline

Each handler executes the same three-stage pipeline:

1. **Scrape** — the handler instantiates its scraper (e.g. `openrouter.New(nil)`) and calls `Fetch(ctx)` to retrieve the latest `[]models.ScrapedModel` from the remote source.
2. **Diff** — `diff.Diff(storedPrices, storedModels, scraped)` compares the incoming data against the values currently stored in PostgreSQL, producing a list of price changes.
3. **Reconcile** — `reconciler.Reconcile(ctx, diffs)` applies the reconciliation rules: single-source changes are queued for a second confirming fetch; multi-source disagreements >5% are flagged to the review queue; confirmed changes are written as immutable records in `price_history`.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/config` | Loads environment variables into a `Config` struct |
| `internal/database` | Opens and pings the PostgreSQL connection pool |
| `internal/worker` | `WorkerStore`, `Handlers`, and task name constants |
| `internal/reconciler` | Mediates all writes to `price_history` |
| `github.com/hibiken/asynq` | Distributed task queue and cron scheduler backed by Redis |
| `github.com/jackc/pgx/v5/pgxpool` | PostgreSQL connection pool |
| `github.com/joho/godotenv` | `.env` file loading |

## Usage

```bash
# Run directly
go run ./cmd/worker

# Build and run
go build -o bin/worker ./cmd/worker
./bin/worker
```

Requires `DATABASE_URL` and `REDIS_URL` environment variables (and optionally `APP_ENV`). See `.env.example`.
