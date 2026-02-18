package handlers

import (
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
//   - provider: filter by provider name
func (h *Handlers) ListChanges(c *fiber.Ctx) error {
	filter := ChangesFilter{
		Provider: c.Query("provider"),
	}

	if raw := c.Query("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return api.NewBadRequest("since must be an ISO 8601 timestamp (e.g. 2006-01-02T15:04:05Z)")
		}
		filter.Since = &t
	}

	changes, err := h.store.ListChanges(c.Context(), filter)
	if err != nil {
		return api.NewInternalError("failed to list changes")
	}

	items := make([]changeResponse, len(changes))
	for i, ch := range changes {
		items[i] = changeResponse{
			ModelID:     ch.ModelID,
			ModelSlug:   ch.ModelSlug,
			Provider:    ch.Provider,
			OldInput:    ch.OldInput,
			OldOutput:   ch.OldOutput,
			NewInput:    ch.NewInput,
			NewOutput:   ch.NewOutput,
			ConfirmedAt: ch.ConfirmedAt,
			Source:      ch.Source,
		}
	}

	// Changes endpoint has aggregated data; use zero-value meta.
	return api.OK(c, items, api.TrustMeta{})
}
