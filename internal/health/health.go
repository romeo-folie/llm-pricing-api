// Package health provides bounded dependency health checks and the liveness
// watchdog that decides when the API process should be restarted.
//
// It exists because of a real outage: /health pinged PostgreSQL and Redis with
// an unbounded context, so when a dependency hung the endpoint hung with it,
// Railway's healthcheck never failed, and the process never exited. Checker
// bounds every probe with a deadline; Watchdog turns repeated failures into a
// restart decision.
package health

import (
	"context"
	"sync"
	"time"
)

// DefaultCheckTimeout bounds a whole health check. It is deliberately short:
// a health probe that cannot answer quickly is reporting on the dependency's
// health, not its own.
const DefaultCheckTimeout = 2 * time.Second

// Status is the outcome of a dependency health check.
type Status struct {
	// DBOK reports whether PostgreSQL answered within the timeout.
	DBOK bool
	// RedisOK reports whether Redis answered within the timeout.
	RedisOK bool
}

// OK reports whether every dependency is reachable.
func (s Status) OK() bool { return s.DBOK && s.RedisOK }

// Checker probes the process dependencies under a single shared deadline.
//
// The pings are injected as functions rather than taken as concrete clients so
// the decision logic can be tested without a database or Redis instance.
type Checker struct {
	pingDB    func(context.Context) error
	pingRedis func(context.Context) error
	timeout   time.Duration
}

// NewChecker returns a Checker that pings both dependencies concurrently under
// one deadline. A non-positive timeout falls back to DefaultCheckTimeout.
func NewChecker(pingDB, pingRedis func(context.Context) error, timeout time.Duration) *Checker {
	if timeout <= 0 {
		timeout = DefaultCheckTimeout
	}
	return &Checker{pingDB: pingDB, pingRedis: pingRedis, timeout: timeout}
}

// Check probes both dependencies and reports each one's reachability.
//
// Both probes run concurrently against a single deadline, so the total time is
// bounded by the timeout however many dependencies hang — a sequential
// implementation would cost the timeout per dependency.
func (c *Checker) Check(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var (
		wg      sync.WaitGroup
		dbOK    bool
		redisOK bool
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		dbOK = c.pingDB(ctx) == nil
	}()
	go func() {
		defer wg.Done()
		redisOK = c.pingRedis(ctx) == nil
	}()
	// The Wait is what makes reading dbOK/redisOK below race-free.
	wg.Wait()

	return Status{DBOK: dbOK, RedisOK: redisOK}
}

// Watchdog turns consecutive unhealthy checks into a restart decision.
//
// It owns no timers: the caller drives it, which keeps the decision logic pure
// and unit-testable. Failures must be *consecutive* — a single healthy
// observation clears the count, so a transient blip cannot accumulate towards
// a restart.
type Watchdog struct {
	threshold   int
	consecutive int
}

// NewWatchdog returns a Watchdog that trips after threshold consecutive
// unhealthy observations. A non-positive threshold is treated as 1.
func NewWatchdog(threshold int) *Watchdog {
	if threshold < 1 {
		threshold = 1
	}
	return &Watchdog{threshold: threshold}
}

// Observe records one health result and reports whether the process should
// exit. A healthy result resets the failure count. After a trip the count also
// resets, so a caller that does not exit immediately gets a fresh window
// rather than tripping on every subsequent failure.
func (w *Watchdog) Observe(healthy bool) bool {
	if healthy {
		w.consecutive = 0
		return false
	}
	w.consecutive++
	if w.consecutive >= w.threshold {
		w.consecutive = 0
		return true
	}
	return false
}
