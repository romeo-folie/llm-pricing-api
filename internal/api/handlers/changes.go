package handlers

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

// changeResponse is the JSON shape for a single price-change event.
type changeResponse struct {
	ModelID     int       `json:"model_id"`
	ModelSlug   string    `json:"model_slug"`
	Provider    string    `json:"provider"`
	OldInput    float64   `json:"old_input"`
	OldOutput   float64   `json:"old_output"`
	NewInput    float64   `json:"new_input"`
	NewOutput   float64   `json:"new_output"`
	ConfirmedAt time.Time `json:"confirmed_at"`
	Source      string    `json:"source"`
}

// ListChanges handles GET /v1/changes.
// Supports query params:
//   - since: ISO 8601 timestamp (default: 24 hours ago)
//   - before: ISO 8601 cursor for pagination (load-more)
//   - limit: max results (default 50, max 200)
//   - provider: filter by provider name
//
// Response headers:
//   - X-Total-Count: total changes in the time window
//   - X-Has-More: "true" or "false"
//   - X-Next-Cursor: confirmed_at of the last item (use as before= for next batch)
func (h *Handlers) ListChanges(c *fiber.Ctx) error {
	filter := ChangesFilter{
		Provider: c.Query("provider"),
		Limit:    50,
	}

	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return api.NewBadRequest("since must be an ISO 8601 timestamp (e.g. 2006-01-02T15:04:05Z)")
		}
		filter.Since = &t
	}

	if raw := c.Query("before"); raw != "" {
		// Accept both RFC3339 (second precision) and RFC3339Nano (sub-second)
		// since X-Next-Cursor emits nanosecond-precision timestamps.
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t, err = time.Parse(time.RFC3339, raw)
		}
		if err != nil {
			return api.NewBadRequest("before must be an ISO 8601 timestamp (e.g. 2006-01-02T15:04:05Z)")
		}
		filter.Before = &t
	}

	if raw := c.Query("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return api.NewBadRequest("limit must be a positive integer")
		}
		if v > MaxPerPage {
			return api.NewBadRequest(fmt.Sprintf("limit must not exceed %d", MaxPerPage))
		}
		filter.Limit = v
	}

	changes, total, err := h.store.ListChanges(c.Context(), filter)
	if err != nil {
		return api.NewInternalError("failed to list changes")
	}

	// Detect has_more: we fetched limit+1 rows in the store; if we got
	// more than limit, there is more data.
	hasMore := len(changes) > filter.Limit
	if hasMore {
		changes = changes[:filter.Limit]
	}

	c.Set("X-Total-Count", strconv.Itoa(total))
	c.Set("X-Has-More", strconv.FormatBool(hasMore))
	if len(changes) > 0 {
		last := changes[len(changes)-1]
		c.Set("X-Next-Cursor", last.ConfirmedAt.UTC().Format(time.RFC3339Nano))
	}

	items := make([]changeResponse, len(changes))
	for i, ch := range changes {
		items[i] = changeResponse(ch)
	}

	return api.OK(c, items, api.TrustMeta{})
}

// summaryResponse is the JSON shape for the changes summary endpoint.
type summaryResponse struct {
	Heatmap      []HeatmapProviderGroup `json:"heatmap"`
	TopMovers    []TopMover             `json:"top_movers"`
	TotalChanges int                    `json:"total_changes"`
	Window       string                 `json:"window"`
}

// GetChangesSummary handles GET /v1/changes/summary.
// Supports query params:
//   - window: "24h" (default), "7d", "30d"
//   - since: ISO 8601 timestamp (overrides window)
//   - provider: filter by provider name
func (h *Handlers) GetChangesSummary(c *fiber.Ctx) error {
	filter := SummaryFilter{
		Provider: c.Query("provider"),
	}

	window := c.Query("window", "24h")
	switch window {
	case "24h":
		t := time.Now().UTC().Add(-24 * time.Hour)
		filter.Since = &t
	case "7d":
		t := time.Now().UTC().Add(-7 * 24 * time.Hour)
		filter.Since = &t
	case "30d":
		t := time.Now().UTC().Add(-30 * 24 * time.Hour)
		filter.Since = &t
	default:
		return api.NewBadRequest("window must be one of: 24h, 7d, 30d")
	}

	// Explicit since overrides window-derived value.
	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return api.NewBadRequest("since must be an ISO 8601 timestamp (e.g. 2006-01-02T15:04:05Z)")
		}
		filter.Since = &t
	}

	summary, err := h.store.GetChangesSummary(c.Context(), filter)
	if err != nil {
		return api.NewInternalError("failed to get changes summary")
	}

	// When an explicit since overrides the window, report "custom" so
	// consumers don't display a misleading label like "7 days".
	if c.Query("since") != "" {
		summary.Window = "custom"
	} else {
		summary.Window = window
	}

	// Ensure slices are never null in JSON.
	if summary.Heatmap == nil {
		summary.Heatmap = []HeatmapProviderGroup{}
	}
	if summary.TopMovers == nil {
		summary.TopMovers = []TopMover{}
	}

	resp := summaryResponse(summary)
	return api.OK(c, resp, api.TrustMeta{})
}
