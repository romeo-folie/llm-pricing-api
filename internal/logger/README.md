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
├── otlp.go         — OTLP log shipping (NewOTLPWriter) and its record mapping
├── logger_test.go  — unit tests covering format selection, level, trace injection
└── otlp_test.go    — unit tests for level/severity mapping, redaction and dropping
```

---

## Key components

### `Config`

| Field | Description |
|-------|-------------|
| `ServiceName` | Embedded as the `"service"` field in every log line |
| `Environment` | `"production"` → JSON; anything else → pretty-print |
| `Level` | Minimum log level; defaults to `zerolog.DebugLevel` when zero |
| `OTLPWriter` | Optional tee target that ships a copy of every JSON line; stdout is always kept |

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

## OTLP shipping to Loki (`otlp.go`)

`NewOTLPWriter(ctx, OTLPConfig)` returns an `io.Writer` that parses each zerolog
JSON line and emits it as an OpenTelemetry log record over `otlploggrpc` on the
**same endpoint as traces**. `logger.New` tees stdout into it, so Railway keeps
its copy if the collector is unavailable.

- **Disabled when no endpoint is set.** The writer is `nil` and the shutdown
  function is a no-op, so local development is untouched. The tee also only
  attaches when output is JSON — the development console writer emits text that
  would parse as nothing.
- **Debug never leaves the process.** `MinLevel` defaults to `InfoLevel`: debug
  stays on stdout, which is the primary control on Loki ingest cost.
- **Never blocks.** Lines go into a bounded queue; when it is full the record is
  dropped and `llm_logs_dropped_total` increments. A request must never wait on
  the logging backend, and a silent drop would be indistinguishable from
  "nothing happened".
- **Trace correlation.** `trace_id`/`span_id` fields from the JSON line are
  rebuilt into a `trace.SpanContext` and passed to `Emit`, which is how Loki
  links a log line to its Tempo trace.
- **Shutdown is idempotent** and flushes the queue, so it can be called on both
  the graceful path and a `defer` — including the `cmd/api` watchdog path that
  calls `os.Exit` and would otherwise skip every defer.
- **Defence-in-depth redaction.** Attribute names matching
  `authorization`, `api_key`, `token`, `secret`, `password`, `cookie`,
  `credential` or `private_key` are replaced with `[redacted]` before shipping.
  This is a safety net, not the control: code must still not log secrets.

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

Never log raw API key values. The primary control is the caller: `logger` does
not know which fields are sensitive. The OTLP shipping path additionally redacts
attribute names that look credential-bearing (`authorization`, `api_key`,
`token`, `secret`, `password`, …), but that is a backstop — treat a redaction as
a bug in the call site, not as permission to log secrets.
