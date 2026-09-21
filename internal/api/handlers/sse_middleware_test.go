package handlers_test

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/middleware"
)

// TestStreamChangesSurvivesMiddlewareUnwind is the regression test for the bug
// that made the SSE leak possible.
//
// With the real middleware stack, otelfiber installs a span context on
// c.UserContext() and cancels it as it unwinds. The handler used to capture
// that context as the stream's request context, so its `<-reqCtx.Done()` case
// fired immediately and every stream was severed right after the initial
// keepalive — while the suite, which mounts the handler without otelfiber,
// passed.
//
// The test consumes the keepalive frame and then asserts the next read blocks.
// A premature EOF (what the old code produced) fails; a read that simply blocks
// is the correct outcome.
func TestStreamChangesSurvivesMiddlewareUnwind(t *testing.T) {
	sse, err := handlers.NewSSEHandler(nil) // nil Redis = heartbeat-only mode
	if err != nil {
		t.Fatalf("NewSSEHandler: %v", err)
	}

	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	app.Use(otelfiber.Middleware())
	app.Use(middleware.RequestTimeout(time.Second))
	app.Get("/v1/stream/changes", sse.StreamChanges)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = app.Listener(ln) }()
	// Bounded: a live SSE connection is expected to outlast the drain window,
	// and the test must not sit through the 30s heartbeat cycle waiting for it.
	t.Cleanup(func() { _ = app.ShutdownWithTimeout(time.Second) })

	reqCtx, cancelReq := context.WithCancel(context.Background())
	t.Cleanup(cancelReq)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+ln.Addr().String()+"/v1/stream/changes", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	br := bufio.NewReader(resp.Body)

	// The keepalive frame is ": ok\n\n" — an event line plus its terminating
	// blank line. Consume both before asserting the stream stays open.
	for _, want := range []string{": ok\n", "\n"} {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading keepalive: got %q, err %v; want %q", line, err, want)
		}
		if line != want {
			t.Fatalf("keepalive line = %q, want %q", line, want)
		}
	}

	readErr := make(chan error, 1)
	go func() {
		_, err := br.ReadString('\n')
		readErr <- err
	}()

	select {
	case err := <-readErr:
		t.Fatalf("stream closed right after the keepalive (read err: %v); it must stay open", err)
	case <-time.After(500 * time.Millisecond):
		// Still open and idle — correct. Cancelling and closing below releases
		// the blocked reader.
	}
}
