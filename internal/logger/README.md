# internal/logger

Provides zerolog-based structured logging helpers for the `llm-pricing-api`
service.  Every log line emitted within a request handler automatically
includes the active OTel `trace_id` and `span_id` so that logs can be
correlated with distributed traces in Grafana / Jaeger / any OTLP backend.

---

## Purpose

- Replaces the standard library `log/slog` used in earlier iterations.
- Outputs **compact JSON** in production (`APP_ENV=production`) and
  **human-readable pretty-print** in all other environments.
- Enriches every log event produced inside a Fiber handler with `trace_id`
  and `span_id` extracted from the active OTel span context — no manual
  field injection required at the call site.
- Follows zerolog's context-storage convention so loggers can be threaded
  through `context.Context` without altering function signatures.

---

## Structure

```
internal/logger/
├── logger.go       — New(), WithContext(), FromContext() implementations
└── logger_test.go  — unit tests covering format selection, level, trace injection
```

---

## Key components

### `Config`

| Field | Description |
|-------|-------------|
| `ServiceName` | Embedded as the `"service"` field in every log line |
| `Environment` | `"production"` → JSON; anything else → pretty-print |
| `Level` | Minimum log level; defaults to `zerolog.DebugLevel` when zero |

### `New(cfg Config) zerolog.Logger`

Constructs a root `zerolog.Logger` writing to `os.Stdout`.  The logger has
`Timestamp()` and `service` permanently attached via `zerolog.With()`.

### `WithContext(ctx, log) context.Context`

Stores `log` inside `ctx` under a private key, following the same contract as
`zerolog.Logger.WithContext` but using the package's own context key to avoid
collisions.

### `FromContext(ctx, fallback) zerolog.Logger`

1. Checks `ctx` for a logger stored by `WithContext`; uses it as the base if
   found, otherwise uses `fallback`.
2. Extracts the active OTel span from `ctx` via `trace.SpanFromContext`.
3. If the span is **recording** (i.e. a real span, not the no-op), appends
   `trace_id` and `span_id` string fields.
4. Returns the enriched logger — no allocation when no span is active.

---

## Log format

### Production (JSON)

```json
{"level":"info","service":"llm-pricing-api","time":"2026-02-18T14:00:00.000Z","trace_id":"abc123...","span_id":"def456...","method":"GET","path":"/v1/models","status":200,"latency_ms":4,"message":"request"}
```

### Development (pretty-print)

```
14:00:00 INF request method=GET path=/v1/models status=200 latency_ms=4 trace_id=abc123... span_id=def456... service=llm-pricing-api
```

---

## Dependencies

| Package | Role |
|---------|------|
| `github.com/rs/zerolog` | Structured JSON / pretty-print logger |
| `go.opentelemetry.io/otel/trace` | `SpanFromContext` — OTel span extraction |

`internal/otel` must be initialised before log lines with trace context are
emitted, otherwise `SpanFromContext` returns a no-op span and the fields are
omitted (safe, not a hard dependency).

---

## Usage

```go
// In main() — create the root logger:
log := logger.New(logger.Config{
    ServiceName: cfg.OTELServiceName,
    Environment: cfg.AppEnv,
})

// Inside a Fiber handler — get a span-enriched logger:
func myHandler(c *fiber.Ctx) error {
    l := logger.FromContext(c.Context(), log)
    l.Info().Str("model_id", id).Msg("fetching model")
    // log line includes trace_id + span_id automatically
    return nil
}

// Store a child logger in a context for deeper call stacks:
ctx = logger.WithContext(ctx, log.With().Str("job", "scrape").Logger())
```

### Security note

Never log raw API key values.  The logger has no automatic redaction — callers
are responsible for excluding sensitive fields from log events.
