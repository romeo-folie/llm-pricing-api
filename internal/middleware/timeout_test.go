package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// contextObservation is what the probe handler reports back about the context
// it was given. It is passed over a channel so the test reads it without
// racing the handler goroutine.
type contextObservation struct {
	deadline    time.Time
	hasDeadline bool
	ctxErr      error
	ran         bool
}

// observeContext runs one request through an app with RequestTimeout applied
// and reports the context the handler observed.
func observeContext(t *testing.T, timeout time.Duration, path string, waitForCancel bool) contextObservation {
	t.Helper()

	obs := make(chan contextObservation, 1)
	app := fiber.New()
	app.Use(RequestTimeout(timeout))
	app.Get(path, func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		dl, ok := ctx.Deadline()

		o := contextObservation{deadline: dl, hasDeadline: ok, ran: true}
		if waitForCancel {
			<-ctx.Done()
			o.ctxErr = ctx.Err()
		}
		obs <- o
		return nil
	})

	resp, err := app.Test(httptest.NewRequest("GET", path, nil))
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	return <-obs
}

// TestRequestTimeoutInstallsDeadline is the core behaviour: a normal request
// gets a bounded context so database and Redis work can be cancelled.
func TestRequestTimeoutInstallsDeadline(t *testing.T) {
	const timeout = 2 * time.Second

	got := observeContext(t, timeout, "/v1/models", false)

	if !got.hasDeadline {
		t.Fatal("handler context has no deadline; the request timeout is not applied")
	}
	remaining := time.Until(got.deadline)
	if remaining <= 0 || remaining > timeout {
		t.Errorf("deadline is %v away, want within (0, %v]", remaining, timeout)
	}
}

// TestRequestTimeoutSkipsStreamingRoutes guards the SSE endpoint: a live feed
// is expected to outlive any request timeout, and cancelling its context would
// sever the stream and could leak the per-key connection slot.
func TestRequestTimeoutSkipsStreamingRoutes(t *testing.T) {
	for _, path := range []string{"/v1/stream/changes", "/v1/stream"} {
		t.Run(path, func(t *testing.T) {
			got := observeContext(t, 2*time.Second, path, false)

			if !got.ran {
				t.Fatal("handler did not run")
			}
			if got.hasDeadline {
				t.Errorf("streaming route %s got a deadline; it must be excluded", path)
			}
		})
	}
}

// TestRequestTimeoutCancelsExpiredContext proves the deadline actually fires,
// which is what frees a connection blocked on pool acquire.
func TestRequestTimeoutCancelsExpiredContext(t *testing.T) {
	got := observeContext(t, 20*time.Millisecond, "/v1/models", true)

	if !errors.Is(got.ctxErr, context.DeadlineExceeded) {
		t.Errorf("context error = %v, want %v", got.ctxErr, context.DeadlineExceeded)
	}
}
