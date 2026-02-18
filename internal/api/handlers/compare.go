package handlers

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

const maxCompareModels = 5

// Compare handles GET /v1/compare?models=id1,id2,...
// Returns side-by-side pricing for up to 5 model IDs.
// Returns 400 if more than 5 IDs are supplied or any ID is not a valid integer.
// Returns 404 if any model ID is not found.
func (h *Handlers) Compare(c *fiber.Ctx) error {
	rawModels := c.Query("models")
	if rawModels == "" {
		return api.NewBadRequest("models query parameter is required")
	}

	parts := strings.Split(rawModels, ",")
	if len(parts) > maxCompareModels {
		return api.NewBadRequest("compare supports at most 5 model IDs")
	}

	ids := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return api.NewBadRequest("each model id must be a positive integer")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return api.NewBadRequest("at least one model id is required")
	}

	models, err := h.store.CompareModels(c.Context(), ids)
	if err != nil {
		if err == ErrNotFound {
			return api.NewNotFound("one or more model IDs were not found")
		}
		return api.NewInternalError("failed to compare models")
	}

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
