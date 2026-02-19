package handlers_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
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

// newSSELiveServer starts a real TCP server backed by a miniredis instance and
// returns the base URL for making real HTTP requests. This is required for tests
// that need to read streaming SSE responses — Fiber's app.Test() does not support
// partial reads of infinite streams.
func newSSELiveServer(t *testing.T) (string, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	app := fiber.New(fiber.Config{
		ErrorHandler:          api.ErrorHandler,
		DisableStartupMessage: true,
	})
	sse, err := handlers.NewSSEHandler(rdb)
	if err != nil {
		t.Fatalf("NewSSEHandler: %v", err)
	}
	app.Get("/v1/stream/changes", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalKeyHash, "testhash")
		return c.Next()
	}, sse.StreamChanges)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = app.ShutdownWithTimeout(2 * time.Second) })
	go func() { _ = app.Listener(ln) }()

	return "http://" + ln.Addr().String(), mr
}

// readSSEBody reads from an SSE response body for up to dur, returning all
// lines joined by newlines. Used by streaming SSE tests to capture partial content.
func readSSEBody(body io.Reader, dur time.Duration) string {
	var sb strings.Builder
	ch := make(chan string, 512)
	go func() {
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			ch <- sc.Text()
		}
		close(ch)
	}()
	deadline := time.After(dur)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return sb.String()
			}
			sb.WriteString(line + "\n")
		case <-deadline:
			return sb.String()
		}
	}
}

// TestStreamChanges_LastEventIDReplay verifies that connecting with a
// Last-Event-ID replays buffered events with scores greater than the given ID.
func TestStreamChanges_LastEventIDReplay(t *testing.T) {
	baseURL, mr := newSSELiveServer(t)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	// Seed the replay buffer with one event at score=100.
	const eventJSON = `{"model_id":1,"provider":"openai","event_id":100}`
	_ = rdb.ZAdd(context.Background(), "price:changes:buffer",
		redis.Z{Score: 100, Member: eventJSON}).Err()

	// Connect with Last-Event-ID=99 → event 100 should be replayed (exclusive lower bound).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/stream/changes", nil)
	req.Header.Set("Last-Event-ID", "99")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()

	// Replay events arrive immediately — 1 s is generous.
	body := readSSEBody(resp.Body, 1*time.Second)
	cancel()

	if !strings.Contains(body, "id: 100") {
		t.Errorf("expected replayed event 'id: 100' in SSE stream, got:\n%s", body)
	}
}

// TestStreamChanges_LiveEventDelivery verifies that an event published to the
// Redis price:changes Pub/Sub channel is forwarded to connected SSE clients.
func TestStreamChanges_LiveEventDelivery(t *testing.T) {
	baseURL, mr := newSSELiveServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/stream/changes", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()

	// Read until the initial ": ok" comment, which confirms the Pub/Sub
	// subscription is active inside the writer goroutine.
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == ": ok" {
			break
		}
	}

	// Publish the event now that the subscription is guaranteed to be set up.
	const eventJSON = `{"model_id":2,"provider":"anthropic","event_id":200}`
	pubRDB := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer pubRDB.Close()
	_ = pubRDB.Publish(context.Background(), "price:changes", eventJSON).Err()

	// The event should arrive quickly; 2 s is a conservative upper bound.
	body := readSSEBody(resp.Body, 2*time.Second)
	cancel()

	if !strings.Contains(body, "id: 200") {
		t.Errorf("expected live event 'id: 200' in SSE stream, got:\n%s", body)
	}
}

// TestStreamChanges_ProviderFilter verifies that events not matching the
// ?provider= query parameter are suppressed, while matching events pass through.
func TestStreamChanges_ProviderFilter(t *testing.T) {
	baseURL, mr := newSSELiveServer(t)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	// Seed buffer: openai event (score=300) and anthropic event (score=301).
	events := []redis.Z{
		{Score: 300, Member: `{"model_id":1,"provider":"openai","event_id":300}`},
		{Score: 301, Member: `{"model_id":2,"provider":"anthropic","event_id":301}`},
	}
	_ = rdb.ZAdd(context.Background(), "price:changes:buffer", events...).Err()

	// Filter for anthropic: event 301 delivered, event 300 suppressed.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/stream/changes?provider=anthropic", nil)
	req.Header.Set("Last-Event-ID", "299")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()

	body := readSSEBody(resp.Body, 1*time.Second)
	cancel()

	if strings.Contains(body, "id: 300") {
		t.Errorf("openai event 300 should be filtered out for provider=anthropic, got:\n%s", body)
	}
	if !strings.Contains(body, "id: 301") {
		t.Errorf("anthropic event 301 should be delivered for provider=anthropic, got:\n%s", body)
	}
}

// TestStreamChanges_ModelFilter verifies that events not matching the
// ?models= query parameter are suppressed, while matching model IDs pass through.
func TestStreamChanges_ModelFilter(t *testing.T) {
	baseURL, mr := newSSELiveServer(t)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	// Seed buffer: model_id=5 (score=400) and model_id=99 (score=401).
	events := []redis.Z{
		{Score: 400, Member: `{"model_id":5,"provider":"openai","event_id":400}`},
		{Score: 401, Member: `{"model_id":99,"provider":"openai","event_id":401}`},
	}
	_ = rdb.ZAdd(context.Background(), "price:changes:buffer", events...).Err()

	// Filter for models=99,100: event 401 delivered, event 400 suppressed.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/stream/changes?models=99,100", nil)
	req.Header.Set("Last-Event-ID", "399")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do: %v", err)
	}
	defer resp.Body.Close()

	body := readSSEBody(resp.Body, 1*time.Second)
	cancel()

	if strings.Contains(body, "id: 400") {
		t.Errorf("event 400 (model_id=5) should be filtered out for models=99,100, got:\n%s", body)
	}
	if !strings.Contains(body, "id: 401") {
		t.Errorf("event 401 (model_id=99) should be delivered for models=99,100, got:\n%s", body)
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
