package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

// taskModalityMap maps ?task= hint values to modality filter values.
// Unrecognised tasks produce an empty string (no modality filter applied).
var taskModalityMap = map[string]string{
	"summarisation":  "text",
	"summarization":  "text",
	"text":           "text",
	"classification": "text",
	"generation":     "text",
	"coding":         "text",
	"code":           "text",
	"vision":         "multimodal",
	"multimodal":     "multimodal",
	"image":          "image",
	"audio":          "audio",
	"embedding":      "embedding",
	"embeddings":     "embedding",
}

// Recommend handles GET /v1/recommend.
// Developer+ only — enforced by RequireTier middleware at route registration.
//
// Optional query parameters:
//   - task:            string hint used to derive a modality filter.
//     Supported mappings: "summarisation"/"summarization"/"text"/"classification"/
//     "generation" → "text"; "vision"/"multimodal" → "multimodal";
//     "image" → "image"; "audio" → "audio"; "embedding"/"embeddings" → "embedding".
//     Unrecognised task values are silently ignored (no modality filter applied).
//   - context:         integer minimum context window (in tokens).
//   - max_price_input: float max input price per 1 million tokens.
//
// Models are ranked by input price ascending. Trust metadata is included
// per model in the response envelope.
//
// Returns 400 for unparseable numeric parameters.
func (h *Handlers) Recommend(c *fiber.Ctx) error {
	task := c.Query("task")
	filter := RecommendFilter{
		Task: task,
	}

	if raw := c.Query("context"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return api.NewBadRequest("context must be a non-negative integer")
		}
		filter.ContextSize = &v
	}

	if raw := c.Query("max_price_input"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			return api.NewBadRequest("max_price_input must be a non-negative number")
		}
		filter.MaxPriceInput = &v
	}

	models, err := h.store.RecommendModels(c.Context(), filter)
	if err != nil {
		return api.NewInternalError("failed to retrieve model recommendations")
	}

	// Apply task→modality filtering in the handler layer because the store
	// returns all matching models regardless of modality; the task is a
	// higher-level hint that we map to a modality here. This keeps the Store
	// interface clean and the mapping logic testable without a DB.
	targetModality := taskModalityMap[task]
	if targetModality != "" {
		filtered := make([]ModelRow, 0, len(models))
		for _, m := range models {
			if m.Modality == targetModality {
				filtered = append(filtered, m)
			}
		}
		models = filtered
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
