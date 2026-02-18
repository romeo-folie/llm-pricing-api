package handlers

import (
	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

// providerResponse is the JSON shape for a single provider item.
type providerResponse struct {
	Name       string `json:"name"`
	ModelCount int    `json:"model_count"`
}

// ListProviders handles GET /v1/providers.
// Returns all providers with their model counts.
func (h *Handlers) ListProviders(c *fiber.Ctx) error {
	providers, err := h.store.ListProviders(c.Context())
	if err != nil {
		return api.NewInternalError("failed to list providers")
	}

	items := make([]providerResponse, len(providers))
	for i, p := range providers {
		items[i] = providerResponse(p)
	}

	// Providers endpoint has no price data, so TrustMeta is zero-value.
	return api.OK(c, items, api.TrustMeta{})
}
