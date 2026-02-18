# internal/api

Shared HTTP response helpers for the LLM pricing REST API. This package provides:

- **RFC 7807 Problem Details** — structured error responses with `Content-Type: application/problem+json`
- **Trust metadata computation** — derive confidence, age, and change-velocity from price history
- **Standard JSON envelope** — consistent `{"data": ..., "meta": {...}}` response wrapper

## Purpose

Every HTTP handler in the `v1` API uses this package to ensure uniform error formatting, response structure, and trust metadata. The package has no I/O dependencies — `ComputeTrustMeta` is a pure function that operates on price history rows already fetched by the calling handler.

## Structure

```
internal/api/
├── problem.go        — RFC 7807 ProblemDetail struct, helper constructors, Fiber ErrorHandler
├── problem_test.go   — Unit tests for all ProblemDetail constructors and the ErrorHandler
├── trust.go          — TrustMeta struct, PriceHistoryRow type, ComputeTrustMeta function
├── trust_test.go     — 100% unit test coverage for ComputeTrustMeta (all confidence paths, velocity)
├── response.go       — Envelope struct, OK() and Created() helpers
├── response_test.go  — Unit tests for OK and Created response helpers
└── README.md         — This file
```

## Key Components

### `ProblemDetail` and `ErrorHandler` (`problem.go`)

`ProblemDetail` implements the [RFC 7807](https://www.rfc-editor.org/rfc/rfc7807) schema:

```go
type ProblemDetail struct {
    Type       string         `json:"type"`
    Title      string         `json:"title"`
    Status     int            `json:"status"`
    Detail     string         `json:"detail,omitempty"`
    Instance   string         `json:"instance,omitempty"`
    Extensions map[string]any `json:"extensions,omitempty"`
}
```

It also implements the `error` interface so handlers can `return api.NewNotFound("item not found")` directly.

`ErrorHandler` is registered as `fiber.Config{ErrorHandler: api.ErrorHandler}` in `cmd/api/main.go`. It:

1. Detects `*ProblemDetail` errors and serialises them directly.
2. Converts native `*fiber.Error` values to the appropriate `ProblemDetail`.
3. Wraps all other errors as 500 Internal Server Error.
4. Always sets `Content-Type: application/problem+json` and `Instance` to the request path.

**Constructor functions** (all produce correctly typed URIs and HTTP status codes):

| Function | Status |
| --- | --- |
| `NewUnauthorized(detail)` | 401 |
| `NewForbidden(detail)` | 403 |
| `NewNotFound(detail)` | 404 |
| `NewBadRequest(detail)` | 400 |
| `NewTooManyRequests(detail)` | 429 |
| `NewInternalError(detail)` | 500 |

### `TrustMeta` and `ComputeTrustMeta` (`trust.go`)

`TrustMeta` is included in every successful API response under the `"meta"` key:

```go
type TrustMeta struct {
    ConfirmedAt    time.Time        `json:"confirmed_at"`
    Source         string           `json:"source"`
    Confidence     models.Confidence `json:"confidence"`
    AgeHours       float64          `json:"age_hours"`
    ChangeVelocity float64          `json:"change_velocity"`
}
```

`ComputeTrustMeta(rows []PriceHistoryRow) TrustMeta` is a **pure function** — no I/O, fully deterministic. Handlers pass it the price history rows they already fetched:

**Confidence rules:**

| Rule | Result |
| --- | --- |
| 2+ distinct source names agree on the latest price pair | `"high"` |
| Single source, confirmed within 24 hours | `"medium"` |
| Single source, confirmed more than 24 hours ago | `"low"` |
| No rows provided | `"low"` (zero TrustMeta) |

**Change velocity** = (number of distinct price-pair values in the last 30 days) / 30. Expressed as changes per day.

`PriceHistoryRow` is the minimal price history representation this package needs. Handlers build it from `models.PriceHistory` rows after resolving source IDs to names.

### `Envelope`, `OK()`, `Created()` (`response.go`)

Standard response wrapper:

```go
type Envelope struct {
    Data any      `json:"data"`
    Meta TrustMeta `json:"meta"`
}
```

Usage in handlers:

```go
func listModels(c *fiber.Ctx) error {
    rows, _ := store.ListModels(c.Context())
    history, _ := store.GetHistory(c.Context(), modelID)
    meta := api.ComputeTrustMeta(toRows(history))
    return api.OK(c, rows, meta)
}
```

## Dependencies

| Dependency | Role |
| --- | --- |
| `github.com/gofiber/fiber/v2` | Fiber context and HTTP status constants |
| `llm-pricing-api/internal/models` | `Confidence` type and constants (`ConfidenceHigh`, `ConfidenceMedium`, `ConfidenceLow`) |

This package has **no database or network dependencies**. It is a pure computation and serialisation layer.

## Usage

### Register the error handler (in `cmd/api/main.go`)

```go
app := fiber.New(fiber.Config{
    ErrorHandler: api.ErrorHandler,
})
```

### Return a typed error from a handler

```go
func getModel(c *fiber.Ctx) error {
    m, err := store.GetModel(c.Context(), c.Params("id"))
    if errors.Is(err, sql.ErrNoRows) {
        return api.NewNotFound("model not found")
    }
    if err != nil {
        return api.NewInternalError("failed to fetch model")
    }
    return api.OK(c, m, api.ComputeTrustMeta(historyRows))
}
```

### Compute trust metadata

```go
history, _ := store.GetPriceHistory(ctx, modelID)
rows := make([]api.PriceHistoryRow, len(history))
for i, h := range history {
    rows[i] = api.PriceHistoryRow{
        InputCostPerToken:  h.InputCostPerToken,
        OutputCostPerToken: h.OutputCostPerToken,
        Source:             sourceNameByID[h.SourceID],
        ConfirmedAt:        h.ConfirmedAt,
        RecordedAt:         h.RecordedAt,
    }
}
meta := api.ComputeTrustMeta(rows)
```
