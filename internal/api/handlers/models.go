package handlers

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"llm-pricing-api/internal/api"
)

// MaxPerPage is the upper bound for the per_page query parameter on list endpoints.
const MaxPerPage = 200

// modelResponse is the JSON shape for a single model item.
type modelResponse struct {
	ID                 int           `json:"id"`
	Provider           string        `json:"provider"`
	Name               string        `json:"name"`
	Slug               string        `json:"slug"`
	Modality           string        `json:"modality"`
	ContextWindow      *int          `json:"context_window"`
	PriceInput         float64       `json:"price_input"`
	PriceOutput        float64       `json:"price_output"`
	UnderlyingProvider *string       `json:"underlying_provider"`
	Meta               api.TrustMeta `json:"meta"`
}

func modelToResponse(r ModelRow) modelResponse {
	return modelResponse{
		ID:                 r.ID,
		Provider:           r.Provider,
		Name:               r.Name,
		Slug:               r.Slug,
		Modality:           r.Modality,
		ContextWindow:      r.ContextWindow,
		PriceInput:         r.PriceInput,
		PriceOutput:        r.PriceOutput,
		UnderlyingProvider: r.UnderlyingProvider,
		Meta:               r.Meta,
	}
}

// ListModels handles GET /v1/models.
// Supports query params: provider, modality, min_context, page, per_page.
// Sets X-Total-Count header with the total number of matching models.
func (h *Handlers) ListModels(c *fiber.Ctx) error {
	filter := ListModelsFilter{
		Provider: c.Query("provider"),
		Modality: c.Query("modality"),
		Page:     1,
		PerPage:  50,
	}

	if raw := c.Query("min_context"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return api.NewBadRequest("min_context must be a non-negative integer")
		}
		filter.MinContext = &v
	}

	if raw := c.Query("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return api.NewBadRequest("page must be a positive integer")
		}
		filter.Page = v
	}

	if raw := c.Query("per_page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return api.NewBadRequest("per_page must be a positive integer")
		}
		if v > MaxPerPage {
			return api.NewBadRequest(fmt.Sprintf("per_page must not exceed %d", MaxPerPage))
		}
		filter.PerPage = v
	}

	models, total, err := h.store.ListModels(c.Context(), filter)
	if err != nil {
		return api.NewInternalError("failed to list models")
	}

	c.Set("X-Total-Count", strconv.Itoa(total))

	items := make([]modelResponse, len(models))
	for i, m := range models {
		items[i] = modelToResponse(m)
	}

	// Aggregate meta: use the most recently confirmed model's meta.
	var meta api.TrustMeta
	for _, m := range models {
		if m.Meta.ConfirmedAt.After(meta.ConfirmedAt) {
			meta = m.Meta
		}
	}

	return api.OK(c, items, meta)
}

// GetModel handles GET /v1/models/:id.
// Accepts either a positive integer ID or a URL slug.
// Returns 404 if the model does not exist.
func (h *Handlers) GetModel(c *fiber.Ctx) error {
	idStr := c.Params("id")

	var model ModelRow
	var err error

	if id, intErr := strconv.Atoi(idStr); intErr == nil && id > 0 {
		model, err = h.store.GetModel(c.Context(), id)
	} else {
		model, err = h.store.GetModelBySlug(c.Context(), idStr)
	}

	if err != nil {
		if err == ErrNotFound {
			return api.NewNotFound("model not found")
		}
		log.Error().Err(err).Str("model_id", idStr).Msg("get model failed")
		return api.NewInternalError("failed to get model")
	}

	return api.OK(c, modelToResponse(model), model.Meta)
}
