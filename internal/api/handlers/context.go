package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Models     []contextModelItem `json:"models"`
	TokenCount int                `json:"token_count"`
	ModelCount int                `json:"model_count"`
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
//
// Optional query parameters:
//   - format=markdown: return a plain-text markdown table instead of the JSON envelope.
func (h *Handlers) GetContext(c *fiber.Ctx) error {
	rows, err := h.store.ListModelsForContext(c.Context(), contextMaxModels)
	if err != nil {
		return api.NewInternalError("failed to retrieve pricing context")
	}

	items := make([]contextModelItem, len(rows))
	for i, r := range rows {
		items[i] = contextModelItem(r)
	}

	// Trim the list until the serialised full response envelope fits within
	// the token budget. We serialize the complete api.Envelope (including the
	// meta wrapper) so the token count matches what the caller actually receives.
	zeroMeta := api.TrustMeta{}
	for len(items) > 0 {
		fullEnvelope := api.Envelope{
			Data: contextResponse{
				Models:     items,
				TokenCount: 0, // placeholder during trim loop
				ModelCount: len(items),
			},
			Meta: zeroMeta,
		}
		b, err := json.Marshal(fullEnvelope)
		if err != nil {
			return api.NewInternalError("failed to serialise context response")
		}
		toks := estimateTokens(b)
		if toks <= contextMaxTokens {
			break
		}
		log.Warn().
			Int("models_before", len(items)).
			Int("tokens", toks).
			Msg("/v1/context response exceeds token budget; trimming model list")
		items = items[:len(items)-1]
	}

	// Build the final response with accurate token count.
	payload := contextResponse{
		Models:     items,
		ModelCount: len(items),
	}

	// If ?format=markdown is requested, return a plain-text markdown table.
	if c.Query("format") == "markdown" {
		md := buildContextMarkdown(items)
		tokenCount := estimateTokens([]byte(md))
		// Prepend metadata summary line.
		header := fmt.Sprintf("> Models: %d | Estimated tokens: %d\n\n", payload.ModelCount, tokenCount)
		c.Set(fiber.HeaderContentType, "text/markdown; charset=utf-8")
		return c.SendString(header + md)
	}

	// Compute token count from the serialised JSON envelope.
	b, err := json.Marshal(api.Envelope{Data: payload, Meta: zeroMeta})
	if err != nil {
		return api.NewInternalError("failed to serialise context response")
	}
	payload.TokenCount = estimateTokens(b)

	return api.OK(c, payload, zeroMeta)
}

// buildContextMarkdown renders items as a GitHub-flavoured markdown table
// suitable for inclusion in agent system prompts.
func buildContextMarkdown(items []contextModelItem) string {
	var sb strings.Builder
	sb.WriteString("| Model | Provider | Input $/1M | Output $/1M | Confidence |\n")
	sb.WriteString("|-------|----------|-----------|-------------|------------|\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("| %s | %s | %.4f | %.4f | %s |\n",
			item.Slug,
			item.Provider,
			item.PriceInput*1_000_000,
			item.PriceOutput*1_000_000,
			item.Confidence,
		))
	}
	return sb.String()
}
