package handlers

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

// historyItemResponse is the JSON shape for one price_history record.
type historyItemResponse struct {
	InputCostPerToken  float64   `json:"input_cost_per_token"`
	OutputCostPerToken float64   `json:"output_cost_per_token"`
	Source             string    `json:"source"`
	ConfirmedAt        time.Time `json:"confirmed_at"`
	RecordedAt         time.Time `json:"recorded_at"`
}

// GetModelHistory handles GET /v1/models/:id/history.
// Developer+ only — enforced by RequireTier middleware at route registration.
//
// Optional query parameters:
//   - from: ISO 8601 timestamp — include records confirmed at or after this time.
//   - to:   ISO 8601 timestamp — include records confirmed at or before this time.
//
// Records are returned in descending confirmed_at order.
// Returns 400 for a non-integer :id or an unparseable date filter.
// Returns 404 if no model with the given ID exists.
func (h *Handlers) GetModelHistory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return api.NewBadRequest("model id must be a positive integer")
	}

	filter := HistoryFilter{}

	if raw := c.Query("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return api.NewBadRequest("from must be an ISO 8601 timestamp (e.g. 2006-01-02T15:04:05Z)")
		}
		filter.From = &t
	}

	if raw := c.Query("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return api.NewBadRequest("to must be an ISO 8601 timestamp (e.g. 2006-01-02T15:04:05Z)")
		}
		filter.To = &t
	}

	// Verify the model exists before querying history; GetModelHistory on a
	// missing model returns an empty slice rather than ErrNotFound.
	model, err := h.store.GetModel(c.Context(), id)
	if err != nil {
		if err == ErrNotFound {
			return api.NewNotFound("model not found")
		}
		return api.NewInternalError("failed to look up model")
	}

	history, err := h.store.GetModelHistory(c.Context(), id, filter)
	if err != nil {
		return api.NewInternalError("failed to retrieve price history")
	}

	items := make([]historyItemResponse, len(history))
	for i, r := range history {
		items[i] = historyItemResponse{
			InputCostPerToken:  r.InputCostPerToken,
			OutputCostPerToken: r.OutputCostPerToken,
			Source:             r.Source,
			ConfirmedAt:        r.ConfirmedAt,
			RecordedAt:         r.RecordedAt,
		}
	}

	// Build trust metadata from the history rows so the response carries the
	// same confidence/age/velocity fields as every other endpoint.
	trustRows := make([]api.PriceHistoryRow, len(history))
	for i, r := range history {
		trustRows[i] = api.PriceHistoryRow{
			InputCostPerToken:  r.InputCostPerToken,
			OutputCostPerToken: r.OutputCostPerToken,
			Source:             r.Source,
			ConfirmedAt:        r.ConfirmedAt,
			RecordedAt:         r.RecordedAt,
		}
	}
	meta := api.ComputeTrustMeta(trustRows)

	// Fall back to the model's own pre-computed meta when there are no
	// history rows (e.g. strict date filter returned nothing).
	if len(history) == 0 {
		meta = model.Meta
	}

	return api.OK(c, items, meta)
}
