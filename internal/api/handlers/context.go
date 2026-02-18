package handlers

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"llm-pricing-api/internal/api"
)

// contextMaxTokens is the hard upper bound on the /v1/context response size
// (measured by the 4-chars-per-token approximation).
const contextMaxTokens = 2100

// contextMaxModels is the initial number of models queried for /v1/context.
// If the serialised response exceeds contextMaxTokens the list is trimmed
// one model at a time until it fits.
const contextMaxModels = 50

// contextModelItem is the per-model JSON payload for /v1/context.
// Fields are intentionally compact to keep the response within the token
// budget suitable for agent system prompts.
type contextModelItem struct {
	ID          int       `json:"id"`
	Provider    string    `json:"provider"`
	Slug        string    `json:"slug"`
	PriceInput  float64   `json:"price_input"`
	PriceOutput float64   `json:"price_output"`
	Confidence  string    `json:"confidence"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// contextResponse is the top-level payload for /v1/context.
type contextResponse struct {
	Models []contextModelItem `json:"models"`
}

// estimateTokens approximates the token count of a JSON byte slice using the
// 4-chars-per-token heuristic. Integer division rounds up so we never
// undercount the size.
func estimateTokens(jsonBytes []byte) int {
	return (len(jsonBytes) + 3) / 4
}

// GetContext handles GET /v1/context.
// Developer+ only — enforced by RequireTier middleware at route registration.
// Caching (10-minute TTL) is applied by the Cache middleware registered at
// the route level; this handler does not write cache headers itself.
//
// The response is a compact JSON pricing snapshot of the top 50 models by ID.
// If the serialised response would exceed 2 100 tokens (4-chars-per-token
// approximation), the model list is trimmed — one model at a time — until it
// fits, and a warning is logged.
func (h *Handlers) GetContext(c *fiber.Ctx) error {
	rows, err := h.store.ListModelsForContext(c.Context(), contextMaxModels)
	if err != nil {
		return api.NewInternalError("failed to retrieve pricing context")
	}

	items := make([]contextModelItem, len(rows))
	for i, r := range rows {
		items[i] = contextModelItem{
			ID:          r.ID,
			Provider:    r.Provider,
			Slug:        r.Slug,
			PriceInput:  r.PriceInput,
			PriceOutput: r.PriceOutput,
			Confidence:  r.Confidence,
			ConfirmedAt: r.ConfirmedAt,
		}
	}

	// Trim the list until the serialised response fits within the token budget.
	for len(items) > 0 {
		payload := contextResponse{Models: items}
		b, err := json.Marshal(payload)
		if err != nil {
			return api.NewInternalError("failed to serialise context response")
		}
		if estimateTokens(b) <= contextMaxTokens {
			break
		}
		log.Warn().
			Int("models_before", len(items)).
			Int("tokens", estimateTokens(b)).
			Msg("/v1/context response exceeds token budget; trimming model list")
		items = items[:len(items)-1]
	}

	return api.OK(c, contextResponse{Models: items}, api.TrustMeta{})
}
