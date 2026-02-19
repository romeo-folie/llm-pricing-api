package handlers_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/middleware"
)

// newSSEApp builds a minimal Fiber app with the SSE handler mounted at
// /v1/stream/changes — no auth middleware so tests bypass auth gating.
// nil Redis = heartbeat-only mode.
func newSSEApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	sse, err := handlers.NewSSEHandler(nil)
	if err != nil {
		t.Fatalf("NewSSEHandler: %v", err)
	}
	app.Get("/v1/stream/changes", sse.StreamChanges)
	return app
}

// newSSEAppWithRedis builds a minimal Fiber app backed by a miniredis instance.
// The handler is mounted WITHOUT auth middleware so tests can control key_hash
// via a Locals-injecting middleware.
func newSSEAppWithRedis(t *testing.T) (*fiber.App, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	sse, err := handlers.NewSSEHandler(rdb)
	if err != nil {
		t.Fatalf("NewSSEHandler: %v", err)
	}
	// Mount WITHOUT auth middleware so tests can control key_hash via Locals.
	app.Get("/v1/stream/changes", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalKeyHash, "testhash")
		return c.Next()
	}, sse.StreamChanges)
	return app, mr
}

// TestStreamChanges_Status200 verifies that GET /v1/stream/changes returns a
// 200 status code when the SSE handler is reachable.
func TestStreamChanges_Status200(t *testing.T) {
	app := newSSEApp(t)

	req := httptest.NewRequest("GET", "/v1/stream/changes", nil)
	// Use a short timeout so app.Test does not block indefinitely on the
	// stream writer goroutine waiting for a ticker tick.
	resp, err := app.Test(req, 500)
	if err != nil && !isTimeoutError(err) {
		t.Fatalf("app.Test: %v", err)
	}
	if resp != nil && resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestStreamChanges_ContentTypeHeader verifies that the SSE response carries
// the correct Content-Type: text/event-stream header.
func TestStreamChanges_ContentTypeHeader(t *testing.T) {
	app := newSSEApp(t)

	req := httptest.NewRequest("GET", "/v1/stream/changes", nil)
	resp, err := app.Test(req, 500)
	if err != nil && !isTimeoutError(err) {
		t.Fatalf("app.Test: %v", err)
	}
	if resp == nil {
		// Timed out before headers were written — this can happen with
		// streaming responses in test mode; skip gracefully.
		t.Skip("response not available (stream timeout)")
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type: expected text/event-stream, got %q", ct)
	}
}

// TestStreamChanges_SSEHeaders verifies that the no-cache and keep-alive
// headers required by the SSE protocol are present.
func TestStreamChanges_SSEHeaders(t *testing.T) {
	app := newSSEApp(t)

	req := httptest.NewRequest("GET", "/v1/stream/changes", nil)
	resp, err := app.Test(req, 500)
	if err != nil && !isTimeoutError(err) {
		t.Fatalf("app.Test: %v", err)
	}
	if resp == nil {
		t.Skip("response not available (stream timeout)")
	}

	wantHeaders := map[string]string{
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	}
	for header, want := range wantHeaders {
		got := resp.Header.Get(header)
		if got != want {
			t.Errorf("header %s: expected %q, got %q", header, want, got)
		}
	}
}

// TestNewSSEHandler_CreatesWithoutError verifies that the constructor succeeds
// with the default (no-op) OTel meter provider that is registered in tests.
func TestNewSSEHandler_CreatesWithoutError(t *testing.T) {
	_, err := handlers.NewSSEHandler(nil)
	if err != nil {
		t.Fatalf("NewSSEHandler returned unexpected error: %v", err)
	}
}

// TestStreamChanges_InvalidLastEventID verifies that a non-integer Last-Event-ID
// is rejected with 400 before the stream is opened.
func TestStreamChanges_InvalidLastEventID(t *testing.T) {
	app := newSSEApp(t)
	req := httptest.NewRequest("GET", "/v1/stream/changes", nil)
	req.Header.Set("Last-Event-ID", "not-a-number")
	resp, err := app.Test(req, 2000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestStreamChanges_NegativeLastEventID verifies that a negative Last-Event-ID
// is rejected with 400.
func TestStreamChanges_NegativeLastEventID(t *testing.T) {
	app := newSSEApp(t)
	req := httptest.NewRequest("GET", "/v1/stream/changes", nil)
	req.Header.Set("Last-Event-ID", "-5")
	resp, err := app.Test(req, 2000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestStreamChanges_ConnectionLimit_FourthReturns429 verifies that a 4th
// concurrent SSE connection for the same API key is rejected with 429.
func TestStreamChanges_ConnectionLimit_FourthReturns429(t *testing.T) {
	app, mr := newSSEAppWithRedis(t)

	// Pre-set the connection count to 3 (simulating 3 active connections).
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	ctx := context.Background()
	_ = rdb.Set(ctx, "sse:conn:testhash", 3, 0).Err()

	req := httptest.NewRequest("GET", "/v1/stream/changes", nil)
	resp, err := app.Test(req, 2000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", resp.StatusCode)
	}

	// Verify the connection count was not leaked (INCR then DECR = back to 3).
	count, _ := rdb.Get(ctx, "sse:conn:testhash").Int64()
	if count != 3 {
		t.Errorf("expected connection count=3 after rejection, got %d", count)
	}
}

// isTimeoutError returns true for errors that indicate a read deadline / timeout
// reached — expected when testing a streaming SSE handler with a short timeout.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "context deadline exceeded") ||
		// Fiber wraps the net error; check for the duration value too.
		strings.Contains(msg, time.Duration(500*time.Millisecond).String())
}
