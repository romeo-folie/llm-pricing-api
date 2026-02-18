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
// Returns 400 for a non-integer :id, an unparseable date filter, or an
// inverted date range (from after to).
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

	// Validate that from is not after to when both are provided.
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return api.NewBadRequest("from must be before to")
	}

	// Use a cheap existence check rather than the full GetModel join.
	exists, err := h.store.ModelExists(c.Context(), id)
	if err != nil {
		return api.NewInternalError("failed to look up model")
	}
	if !exists {
		return api.NewNotFound("model not found")
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

	// Build trust metadata from the full unfiltered history so that confidence,
	// age, and change_velocity reflect the model's actual data quality rather
	// than the subset selected by the caller's date window.
	metaHistory := history
	if filter.From != nil || filter.To != nil {
		metaHistory, err = h.store.GetModelHistory(c.Context(), id, HistoryFilter{})
		if err != nil {
			// Non-fatal: fall back to filtered history for meta computation.
			metaHistory = history
		}
	}
	trustRows := make([]api.PriceHistoryRow, len(metaHistory))
	for i, r := range metaHistory {
		trustRows[i] = api.PriceHistoryRow{
			InputCostPerToken:  r.InputCostPerToken,
			OutputCostPerToken: r.OutputCostPerToken,
			Source:             r.Source,
			ConfirmedAt:        r.ConfirmedAt,
			RecordedAt:         r.RecordedAt,
		}
	}
	meta := api.ComputeTrustMeta(trustRows)

	return api.OK(c, items, meta)
}
