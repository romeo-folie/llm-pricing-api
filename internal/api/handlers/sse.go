package handlers

import (
	"bufio"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// SSEHandler holds the OTel UpDownCounter for active SSE connections.
type SSEHandler struct {
	activeConns metric.Int64UpDownCounter
}

// NewSSEHandler creates an SSEHandler initialised with an OTel UpDownCounter.
// The counter instrument name is "llm_pricing.sse.active_connections".
func NewSSEHandler() (*SSEHandler, error) {
	meter := otel.GetMeterProvider().Meter("llm-pricing-api")
	counter, err := meter.Int64UpDownCounter(
		"llm_pricing.sse.active_connections",
		metric.WithDescription("Number of active SSE connections"),
	)
	if err != nil {
		return nil, err
	}
	return &SSEHandler{activeConns: counter}, nil
}

// StreamChanges implements GET /v1/stream/changes.
//
// This is a Phase 2 stub: it sets the correct SSE response headers, increments
// the active-connection counter, sends a single keepalive SSE comment
// (": ok\n\n"), then holds the connection open by sending a heartbeat comment
// every 30 seconds until the underlying TCP connection is closed by the client.
//
// Full reconnection logic (Last-Event-ID, real price-change events) is deferred
// to Phase 4.
func (h *SSEHandler) StreamChanges(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	ctx := c.Context()
	h.activeConns.Add(ctx, 1)
	defer h.activeConns.Add(ctx, -1)

	c.Status(fiber.StatusOK)
	c.Response().SetBodyStreamWriter(func(w *bufio.Writer) {
		// Send initial keepalive comment so the client knows the stream is live.
		if _, err := fmt.Fprint(w, ": ok\n\n"); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}

		// Poll on a 30-second heartbeat; write errors signal that the client
		// has closed the connection, at which point we return and defer fires.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	})

	return nil
}
