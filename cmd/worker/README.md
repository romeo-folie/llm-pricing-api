# cmd/worker

Background job worker entrypoint for the LLM Pricing Platform.

## Purpose

Runs an asynq worker server that processes asynchronous tasks from the Redis-backed job queue. In Phase 1 this will handle scraper jobs, reconciliation tasks, and webhook delivery. Currently a scaffold with no registered task handlers.

## Structure

```
cmd/worker/
  main.go      # Worker entrypoint — config, asynq server setup, shutdown
  README.md    # This file
```

## Key Components

- **`main()`** — Loads `.env` via godotenv, reads config, creates an asynq server (concurrency 10) connected to Redis, sets up a `ServeMux` for task handler registration, and starts the worker. Blocks on `SIGINT`/`SIGTERM` for graceful shutdown via `srv.Shutdown()`.

## Dependencies

| Dependency | Role |
|---|---|
| `internal/config` | Loads environment variables into a `Config` struct |
| `github.com/hibiken/asynq` | Distributed task queue backed by Redis |
| `github.com/joho/godotenv` | `.env` file loading |

## Usage

```bash
# Run directly
go run ./cmd/worker

# Via Makefile
make worker
```

Requires `DATABASE_URL` and `REDIS_URL` environment variables. See `.env.example`.
