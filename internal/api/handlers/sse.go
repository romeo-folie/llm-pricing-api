package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/middleware"
)

const (
	// sseConnKeyPrefix is the Redis key prefix for per-API-key connection counts.
	// Full key: sse:conn:{key_hash}
	sseConnKeyPrefix = "sse:conn:"

	// sseMaxConnsPerKey is the maximum concurrent SSE connections per API key.
	sseMaxConnsPerKey = 3

	// sseConnTTL is the safety-net TTL on the connection count key.
	// Prevents count leaks if a server crash prevents the defer from running.
	sseConnTTL = 5 * time.Minute

	// ssePubSubChannel is the Redis Pub/Sub channel name for price-change events.
	ssePubSubChannel = "price:changes"

	// sseReplayBufferKey is the sorted-set key used for Last-Event-ID replay.
	sseReplayBufferKey = "price:changes:buffer"

	// sseHeartbeatInterval is how often the server sends a keep-alive comment.
	sseHeartbeatInterval = 30 * time.Second

	// sseWriteTimeout bounds a single write to the client.
	//
	// A peer that disappears without sending an RST leaves the socket writable
	// as far as the kernel is concerned, so an unbounded write blocks forever:
	// the stream loop never returns and its goroutines, Redis Pub/Sub
	// subscription and per-key connection slot leak permanently. Two heartbeat
	// intervals is long enough that an idle-but-healthy client is never cut off.
	sseWriteTimeout = 2 * sseHeartbeatInterval
)

// SSEHandler holds dependencies for the SSE stream endpoint.
type SSEHandler struct {
	redisClient   *redis.Client
	activeConns   metric.Int64UpDownCounter
	eventsEmitted metric.Int64Counter
}

// NewSSEHandler creates an SSEHandler with the given Redis client and registers
// OTel instruments. rdb may be nil — connection limiting and Pub/Sub are disabled
// (heartbeat-only mode) when no client is provided.
func NewSSEHandler(rdb *redis.Client) (*SSEHandler, error) {
	meter := otel.GetMeterProvider().Meter("llm-pricing-api")

	activeConns, err := meter.Int64UpDownCounter(
		"llm_pricing.sse.active_connections",
		metric.WithDescription("Number of active SSE connections"),
	)
	if err != nil {
		return nil, fmt.Errorf("create active_connections counter: %w", err)
	}

	eventsEmitted, err := meter.Int64Counter(
		"llm_pricing.sse.events_emitted_total",
		metric.WithDescription("Total SSE events emitted, labelled by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("create events_emitted counter: %w", err)
	}

	return &SSEHandler{
		redisClient:   rdb,
		activeConns:   activeConns,
		eventsEmitted: eventsEmitted,
	}, nil
}

// sseFilters holds the parsed query-param filters for an SSE connection.
type sseFilters struct {
	provider string              // empty = no filter
	modelIDs map[string]struct{} // empty = no filter; key = stringified model_id
}

// parseSSEFilters extracts ?provider= and ?models= from the request.
func parseSSEFilters(c *fiber.Ctx) sseFilters {
	f := sseFilters{}
	if p := c.Query("provider"); p != "" {
		f.provider = strings.ToLower(p)
	}
	if m := c.Query("models"); m != "" {
		parts := strings.Split(m, ",")
		f.modelIDs = make(map[string]struct{}, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				f.modelIDs[p] = struct{}{}
			}
		}
	}
	return f
}

// sseEventPayload is the minimal shape we need for filtering. It mirrors the
// SSEEvent struct from the reconciler package without creating an import cycle.
type sseEventPayload struct {
	ModelID  int    `json:"model_id"`
	Provider string `json:"provider"`
	EventID  int64  `json:"event_id"`
}

// matchesFilter reports whether the raw JSON event payload passes the filters.
// Returns (parsed payload, matched). If JSON is invalid, returns false.
func matchesFilter(rawJSON string, f sseFilters) (sseEventPayload, bool) {
	var ev sseEventPayload
	if err := json.Unmarshal([]byte(rawJSON), &ev); err != nil {
		return ev, false
	}
	if f.provider != "" && strings.ToLower(ev.Provider) != f.provider {
		return ev, false
	}
	if len(f.modelIDs) > 0 {
		if _, ok := f.modelIDs[strconv.Itoa(ev.ModelID)]; !ok {
			return ev, false
		}
	}
	return ev, true
}

// writeWithDeadline writes payload and flushes it, bounding the whole write
// with a socket deadline.
//
// The deadline is what makes an abandoned client observable: writing to a
// half-open connection otherwise blocks indefinitely, and the only exit from
// the stream loop is a write error. Note that w wraps fasthttp's in-memory
// pipe rather than the socket, so the deadline bounds fasthttp's own socket
// write; that failure closes the pipe reader, which unblocks the pending Flush.
func writeWithDeadline(conn net.Conn, w *bufio.Writer, payload string) error {
	if conn != nil {
		_ = conn.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	}
	if _, err := fmt.Fprint(w, payload); err != nil {
		return err
	}
	return w.Flush()
}

// writeSSEEvent writes a single SSE event frame:
//
//	id: {event_id}
//	data: {json_payload}
//	(blank line)
func writeSSEEvent(conn net.Conn, w *bufio.Writer, eventID int64, payload string) error {
	return writeWithDeadline(conn, w, fmt.Sprintf("id: %d\ndata: %s\n\n", eventID, payload))
}

// StreamChanges implements GET /v1/stream/changes.
//
// On connect:
//  1. Parse ?provider= and ?models= filters.
//  2. Validate Last-Event-ID header (must be int64 or absent).
//  3. Enforce per-key connection limit (max 3 concurrent; 429 on 4th).
//  4. Enter stream writer goroutine.
//
// Inside the writer goroutine:
//   - Replay missed events from the sorted-set buffer if Last-Event-ID is present.
//   - Subscribe to Redis price:changes channel and stream filtered events.
//   - Send 30-second heartbeat comments between events.
//   - Decrement connection count on exit (defer).
func (h *SSEHandler) StreamChanges(c *fiber.Ctx) error {
	// --- 1. Parse query filters ---
	filters := parseSSEFilters(c)

	// --- 2. Validate Last-Event-ID ---
	var lastEventID int64
	hasLastEventID := false
	if leid := c.Get("Last-Event-ID"); leid != "" {
		n, err := strconv.ParseInt(leid, 10, 64)
		if err != nil || n < 0 {
			return api.NewBadRequest("Last-Event-ID must be a non-negative integer")
		}
		lastEventID = n
		hasLastEventID = true
	}

	// --- 3. Per-key connection limit ---
	keyHash, _ := c.Locals(middleware.LocalKeyHash).(string)
	// counted records whether this connection actually incremented the per-key
	// counter, so the cleanup defer never decrements a counter that was never
	// incremented — which would drive the key negative and under-count.
	counted := false
	if keyHash != "" && h.redisClient != nil {
		connKey := sseConnKeyPrefix + keyHash
		count, err := h.redisClient.Incr(c.UserContext(), connKey).Result()
		if err != nil {
			// Redis error — allow connection through rather than blocking all clients.
			count = 1
		} else {
			counted = true
			// Refresh safety-net TTL on every connect.
			_ = h.redisClient.Expire(c.UserContext(), connKey, sseConnTTL).Err()
		}
		if count > sseMaxConnsPerKey {
			// Over limit — decrement and reject.
			if counted {
				_ = h.redisClient.Decr(c.UserContext(), connKey).Err()
			}
			return api.NewTooManyRequests("maximum concurrent SSE connections per API key (3) exceeded")
		}
	}

	// --- 4. Set SSE response headers ---
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	h.activeConns.Add(c.UserContext(), 1)

	// The stream outlives this handler: fasthttp runs the body stream writer
	// after the middleware chain has unwound, so no request-derived context is
	// usable here. otelfiber cancels its span context as it unwinds, and
	// Fiber's default user context is a Background that is never cancelled.
	// Give the stream a context it owns, cancelled by its own cleanup defer.
	streamCtx, cancelStream := context.WithCancel(context.Background())

	// Capture the underlying connection: unlike c.Context() it is not recycled
	// when the handler returns, and it is what carries the write deadline.
	conn := c.Context().Conn()

	rdb := h.redisClient

	c.Status(fiber.StatusOK)
	c.Response().SetBodyStreamWriter(func(w *bufio.Writer) {
		// Cleanup on exit: release the stream context, decrement the OTel gauge,
		// and hand back the per-key slot if this connection took one.
		defer cancelStream()
		defer h.activeConns.Add(context.Background(), -1)
		if counted {
			connKey := sseConnKeyPrefix + keyHash
			defer func() { _ = rdb.Decr(context.Background(), connKey).Err() }()
		}

		// --- 5. Send initial keepalive ---
		if err := writeWithDeadline(conn, w, ": ok\n\n"); err != nil {
			return
		}

		// --- 6. Replay missed events from buffer ---
		if hasLastEventID && rdb != nil {
			// Use exclusive lower bound "(lastEventID" to skip the already-seen event.
			minScore := fmt.Sprintf("(%d", lastEventID)
			members, err := rdb.ZRangeArgs(streamCtx, redis.ZRangeArgs{
				Key:     sseReplayBufferKey,
				Start:   minScore,
				Stop:    "+inf",
				ByScore: true,
			}).Result()
			if err == nil {
				for _, member := range members {
					ev, ok := matchesFilter(member, filters)
					if !ok {
						continue
					}
					if err := writeSSEEvent(conn, w, ev.EventID, member); err != nil {
						return
					}
					h.eventsEmitted.Add(context.Background(), 1,
						metric.WithAttributes(attribute.String("provider", ev.Provider)))
				}
			}
			// On replay error: log nothing (fire-and-forget), just enter live mode.
		}

		// --- 7. Subscribe and stream live events ---
		if rdb == nil {
			// No Redis — heartbeat-only mode.
			h.heartbeatLoop(streamCtx, conn, w)
			return
		}

		sub := rdb.Subscribe(streamCtx, ssePubSubChannel)
		defer func() { _ = sub.Close() }()

		msgCh := sub.Channel()
		ticker := time.NewTicker(sseHeartbeatInterval)
		defer ticker.Stop()

		// The loop ends when a write fails (bounded by sseWriteTimeout) or when
		// Redis closes the channel. There is deliberately no context case here:
		// a request-derived context is unusable in this goroutine, and the
		// socket deadline is what turns an abandoned peer into a write error.
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					// Channel closed (Redis disconnected). Fall back to heartbeat loop.
					h.heartbeatLoop(streamCtx, conn, w)
					return
				}
				ev, ok := matchesFilter(msg.Payload, filters)
				if !ok {
					continue
				}
				if err := writeSSEEvent(conn, w, ev.EventID, msg.Payload); err != nil {
					return
				}
				h.eventsEmitted.Add(context.Background(), 1,
					metric.WithAttributes(attribute.String("provider", ev.Provider)))

			case <-ticker.C:
				if err := writeWithDeadline(conn, w, ": heartbeat\n\n"); err != nil {
					return
				}
				// Keep the per-key counter alive while this stream is genuinely
				// live. A stream blocked on a dead peer cannot reach this tick,
				// so refreshing cannot pin the slot of a leaked connection —
				// whereas dropping it lets the key expire beneath a healthy
				// long-lived stream, silently defeating the per-key cap and
				// leaving a later DECR to recreate the key at -1.
				if counted {
					_ = rdb.Expire(context.Background(), sseConnKeyPrefix+keyHash, sseConnTTL).Err()
				}
			}
		}
	})

	return nil
}

// heartbeatLoop sends 30-second heartbeat comments until the context is cancelled.
// Used as a fallback when Redis is unavailable.
func (h *SSEHandler) heartbeatLoop(ctx context.Context, conn net.Conn, w *bufio.Writer) {
	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writeWithDeadline(conn, w, ": heartbeat\n\n"); err != nil {
				return
			}
		}
	}
}
