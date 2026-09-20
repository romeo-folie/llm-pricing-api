package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// streamingRoutePrefixes lists route prefixes that must never be bounded by
// RequestTimeout. An SSE connection is expected to outlive any request
// deadline, and cancelling its context would sever the stream mid-flight and
// risk leaking the per-key connection slot that the handler decrements on exit.
var streamingRoutePrefixes = []string{"/v1/stream"}

// RequestTimeout bounds how long a normal request may run by installing a
// deadline on the request's user context.
//
// The deadline only cancels work that is handed a derived context, so handlers
// and middleware must use c.UserContext() rather than c.Context() for database
// and Redis calls. fasthttp's c.Context() carries no deadline, so a caller that
// keeps using it stays unbounded by design — silently, which is why the
// handlers were switched in the same change.
//
// This middleware spawns no goroutine: it narrows the existing context and lets
// the handler chain unwind normally. A handler that ignores its context is
// therefore still not interrupted, but anything that respects it — including
// every pgx pool acquire and query — is released at the deadline.
func RequestTimeout(timeout time.Duration) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if isStreamingRoute(c.Path()) {
			return c.Next()
		}

		// Restore the previous context when the chain unwinds: leaving a
		// cancelled context installed would break the middleware contract for
		// any outer middleware reading c.UserContext() after c.Next().
		prev := c.UserContext()
		ctx, cancel := context.WithTimeout(prev, timeout)
		defer func() {
			cancel()
			c.SetUserContext(prev)
		}()
		c.SetUserContext(ctx)

		return c.Next()
	}
}

// isStreamingRoute reports whether path is a long-lived streaming endpoint.
// The comparison is case-insensitive because Fiber's router is: /V1/STREAM/…
// reaches the same handler, so a case-sensitive check would leave that path
// bounded by the request timeout.
func isStreamingRoute(path string) bool {
	path = strings.ToLower(path)
	for _, prefix := range streamingRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
