# internal/otel

Initialises the OpenTelemetry SDK for the `llm-pricing-api` service and
registers the global `TracerProvider` and `TextMapPropagator` that all
instrumentation libraries in the process depend on.

---

## Purpose

Provides a single `Init()` call that:

1. Builds an OTel `Resource` with `service.name`, `service.version`, and
   `deployment.environment` attributes.
2. Creates an OTLP/gRPC trace exporter when `OTEL_EXPORTER_OTLP_ENDPOINT` is
   set; falls back to a **no-op** provider when the variable is empty so the
   service starts safely without an external collector.
3. Registers the provider and a composite `TraceContext` + `Baggage`
   propagator as global singletons consumed by `otelfiber`, `otelpgx`, and
   `redisotel`.
4. Returns a `shutdown` function that must be deferred in `main()` to flush
   buffered spans before process exit.

---

## Structure

```
internal/otel/
├── otel.go       — Config struct + Init() function
└── otel_test.go  — unit tests (no-op path, resource attributes, double-shutdown)
```

---

## Key components

### `Config`

| Field | Description |
|-------|-------------|
| `ServiceName` | Logical service name embedded in every trace (e.g. `llm-pricing-api`) |
| `ServiceVersion` | Semver string embedded in resource attributes |
| `Environment` | Deployment environment, e.g. `development` / `production` |
| `OTLPEndpoint` | gRPC collector address (e.g. `localhost:4317`); empty → no-op |

### `Init(ctx, Config) (shutdown func(context.Context) error, err error)`

Configures and globally registers the SDK.  When `OTLPEndpoint` is non-empty
an `otlptracegrpc` exporter is created with `WithInsecure()` — TLS is
terminated at the collector sidecar in production.  Spans are batched via
`sdktrace.WithBatcher`.

The returned `shutdown` function drains the exporter queue and must be called
before process exit.  Calling it multiple times is safe.

---

## Dependencies

| Package | Role |
|---------|------|
| `go.opentelemetry.io/otel/sdk` | Core SDK — TracerProvider, BatchSpanProcessor |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | OTLP gRPC exporter |
| `go.opentelemetry.io/otel/sdk/resource` | Resource builder |
| `go.opentelemetry.io/otel/semconv/v1.26.0` | Semantic convention keys |
| `go.opentelemetry.io/otel/propagation` | TraceContext + Baggage propagators |

Downstream packages that consume the global provider:

- `github.com/gofiber/contrib/otelfiber/v2` — HTTP server spans
- `github.com/exaring/otelpgx` — PostgreSQL query spans
- `github.com/redis/go-redis/extra/redisotel/v9` — Redis command spans

---

## Usage

```go
// In main():
otelShutdown, err := internalotel.Init(ctx, internalotel.Config{
    ServiceName:    cfg.OTELServiceName,   // "llm-pricing-api"
    ServiceVersion: "0.1.0",
    Environment:    cfg.AppEnv,            // "development" | "production"
    OTLPEndpoint:   cfg.OTELEndpoint,      // OTEL_EXPORTER_OTLP_ENDPOINT
})
if err != nil {
    log.Fatal().Err(err).Msg("failed to initialise OTel SDK")
}
defer func() {
    shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = otelShutdown(shutCtx)
}()
```

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | gRPC address of OTLP collector; unset → no-op SDK |
| `OTEL_SERVICE_NAME` | `llm-pricing-api` | Overrides the service name read by `config.Load()` |
