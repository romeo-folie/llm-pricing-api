package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
	provider string             // empty = no filter
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

// writeSSEEvent writes a single SSE event frame to w:
//
//	id: {event_id}
//	data: {json_payload}
//	(blank line)
func writeSSEEvent(w *bufio.Writer, eventID int64, payload string) error {
	if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", eventID, payload); err != nil {
		return err
	}
	return w.Flush()
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
	if keyHash != "" && h.redisClient != nil {
		connKey := sseConnKeyPrefix + keyHash
		count, err := h.redisClient.Incr(c.Context(), connKey).Result()
		if err != nil {
			// Redis error — allow connection through rather than blocking all clients.
			count = 1
		} else {
			// Refresh safety-net TTL on every connect.
			_ = h.redisClient.Expire(c.Context(), connKey, sseConnTTL).Err()
		}
		if count > sseMaxConnsPerKey {
			// Over limit — decrement and reject.
			_ = h.redisClient.Decr(c.Context(), connKey).Err()
			return api.NewTooManyRequests("maximum concurrent SSE connections per API key (3) exceeded")
		}
	}

	// --- 4. Set SSE response headers ---
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	h.activeConns.Add(c.Context(), 1)

	// Capture request context before handing off to the writer goroutine.
	// fasthttp may recycle c.Context() after the handler returns.
	reqCtx := c.UserContext()
	rdb := h.redisClient

	c.Status(fiber.StatusOK)
	c.Response().SetBodyStreamWriter(func(w *bufio.Writer) {
		// Cleanup on exit: decrement connection count and OTel gauge.
		defer h.activeConns.Add(context.Background(), -1)
		if keyHash != "" && rdb != nil {
			connKey := sseConnKeyPrefix + keyHash
			defer func() { _ = rdb.Decr(context.Background(), connKey).Err() }()
		}

		// --- 5. Send initial keepalive ---
		if _, err := fmt.Fprint(w, ": ok\n\n"); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}

		// --- 6. Replay missed events from buffer ---
		if hasLastEventID && rdb != nil {
			// Use exclusive lower bound "(lastEventID" to skip the already-seen event.
			minScore := fmt.Sprintf("(%d", lastEventID)
			members, err := rdb.ZRangeArgs(reqCtx, redis.ZRangeArgs{
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
					if err := writeSSEEvent(w, ev.EventID, member); err != nil {
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
			h.heartbeatLoop(reqCtx, w)
			return
		}

		sub := rdb.Subscribe(reqCtx, ssePubSubChannel)
		defer func() { _ = sub.Close() }()

		msgCh := sub.Channel()
		ticker := time.NewTicker(sseHeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-reqCtx.Done():
				return

			case msg, ok := <-msgCh:
				if !ok {
					// Channel closed (Redis disconnected). Fall back to heartbeat loop.
					h.heartbeatLoop(reqCtx, w)
					return
				}
				ev, ok := matchesFilter(msg.Payload, filters)
				if !ok {
					continue
				}
				if err := writeSSEEvent(w, ev.EventID, msg.Payload); err != nil {
					return
				}
				h.eventsEmitted.Add(context.Background(), 1,
					metric.WithAttributes(attribute.String("provider", ev.Provider)))

			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
				// Refresh the connection-count key TTL on every heartbeat so the
				// safety-net expiry only kicks in after a real crash (i.e. after
				// sseConnTTL of silence, meaning all active connections have closed
				// or the process has died without running their defers).
				if keyHash != "" && rdb != nil {
					_ = rdb.Expire(context.Background(), sseConnKeyPrefix+keyHash, sseConnTTL).Err()
				}
			}
		}
	})

	return nil
}

// heartbeatLoop sends 30-second heartbeat comments until the context is cancelled.
// Used as a fallback when Redis is unavailable.
func (h *SSEHandler) heartbeatLoop(ctx context.Context, w *bufio.Writer) {
	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	}
}
