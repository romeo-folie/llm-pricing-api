# internal/health

Bounded dependency health checks and the liveness watchdog for the API process.

## Purpose

Provides the two things needed to notice — and recover from — a wedged process:

- **`Checker`** — probes PostgreSQL and Redis under a single, short deadline.
- **`Watchdog`** — turns consecutive failed probes into a restart decision.

It exists because of the four-day silent outage tracked in issue #183. `/health` pinged both
dependencies with an unbounded context, so when a dependency hung the endpoint hung with it:
Railway's healthcheck never failed, the process never exited, and `restartPolicyType: ON_FAILURE`
never fired. Nothing in the system was watching the thing that was broken.

## Structure

```
internal/health/
  health.go       # Checker, Status, Watchdog
  health_test.go  # Tests driven by injected ping functions; no database or Redis required
  README.md       # This file
```

## Key Components

### `Checker`

```go
func NewChecker(pingDB, pingRedis func(context.Context) error, timeout time.Duration) *Checker
func (c *Checker) Check(ctx context.Context) Status
```

`Check` runs both pings **concurrently** under one shared deadline, so the total time is bounded by
`timeout` however many dependencies hang. A sequential implementation would cost one timeout per
dependency, which is why a regression test asserts the total stays within roughly one budget rather
than two.

`Status` reports each dependency separately (`DBOK`, `RedisOK`) so `/health` can say *which* side is
down; `Status.OK()` is the single answer the watchdog consumes.

Ping behaviour is injected as functions rather than taken as concrete clients, so the package is
testable with no database or Redis instance. The tests cover healthy, either-dependency-down,
both-down, and permanently hanging dependencies.

### `Watchdog`

```go
func NewWatchdog(threshold int) *Watchdog
func (w *Watchdog) Observe(healthy bool) bool
```

`Observe` reports whether the process should exit. Failures must be **consecutive** — any healthy
observation clears the count, so a transient blip cannot accumulate towards a restart. After a trip
the count also clears, so a caller that does not exit immediately gets a fresh window instead of
tripping on every later failure.

The watchdog owns no timers. The caller drives it, which is what keeps the decision logic pure and
unit-testable.

## Dependencies

Standard library only (`context`, `sync`, `time`). It deliberately does not import `pgx` or
`go-redis`; callers inject the ping closures.

## Usage

```go
checker := health.NewChecker(
    db.Ping,
    func(ctx context.Context) error { return redisClient.Ping(ctx).Err() },
    health.DefaultCheckTimeout, // 2s
)

// /health — bounded, so an unreachable dependency yields a fast 503 rather than a hang
st := checker.Check(c.Context())
// st.OK(), st.DBOK, st.RedisOK

// liveness watchdog, driven by a ticker the caller owns
wd := health.NewWatchdog(3)
if wd.Observe(checker.Check(ctx).DBOK) {
    log.Error().Msg("database unreachable repeatedly — shutting down for restart")
    return errWatchdogTripped // caller shuts down, flushes traces, then exits non-zero
}
```

`cmd/api` wires both: `/health` uses the checker directly, and a 30-second ticker drives the
watchdog. It watches the **database** specifically — pool exhaustion is what wedges the process
(`Ping` must acquire a connection), whereas a Redis outage is survivable because cache, rate limiting
and auth all fail open, and restarting on it would turn a degraded-but-serving API into a crash loop.
The watchdog *reports* a trip rather than exiting from its goroutine, so `main` runs the normal
shutdown path and flushes traces before exiting non-zero for Railway's `ON_FAILURE` policy.
