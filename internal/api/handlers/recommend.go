package handlers

import (
	"sort"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/intelligence"
)

// taskModalityMap maps ?task= hint values to modality filter values.
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

// recommendModelResponse extends modelResponse with capability scoring data.
type recommendModelResponse struct {
	modelResponse
	Scores          map[string]float64              `json:"scores,omitempty"`
	Rationale       string                          `json:"rationale,omitempty"`
	BenchmarksCited []intelligence.BenchmarkCitation `json:"benchmarks_cited,omitempty"`
	Warnings        []string                        `json:"warnings,omitempty"`
	Fallback        bool                            `json:"fallback,omitempty"`
}

// Recommend handles GET /v1/recommend.
func (h *Handlers) Recommend(c *fiber.Ctx) error {
	task := c.Query("task")
	filter := RecommendFilter{Task: task}

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

	if uc := c.Query("use_case"); uc != "" {
		if !intelligence.ValidUseCases[intelligence.UseCase(uc)] {
			return api.NewBadRequest("use_case must be one of: finance, coding, agentic, writing, video, reasoning, general")
		}
		filter.UseCase = uc
	}
	if p := c.Query("priority"); p != "" {
		if !intelligence.ValidPriorities[intelligence.Priority(p)] {
			return api.NewBadRequest("priority must be one of: quality, cost, speed, balanced")
		}
		filter.Priority = p
	}
	filter.RequiresTools = c.Query("requires_tools") == "true"
	filter.RequiresStructuredOutput = c.Query("requires_structured_output") == "true"

	topK := 5
	if raw := c.Query("top_k"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 20 {
			return api.NewBadRequest("top_k must be an integer between 1 and 20")
		}
		topK = v
	}
	filter.TopK = topK

	models, err := h.store.RecommendModels(c.Context(), filter)
	if err != nil {
		return api.NewInternalError("failed to retrieve model recommendations")
	}

	if mod := taskModalityMap[task]; mod != "" {
		filtered := make([]ModelRow, 0, len(models))
		for _, m := range models {
			if m.Modality == mod {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}

	if filter.UseCase == "" {
		return h.recommendPriceBased(c, models)
	}
	return h.recommendCapabilityBased(c, models, filter)
}

func (h *Handlers) recommendPriceBased(c *fiber.Ctx, models []ModelRow) error {
	items := make([]modelResponse, len(models))
	for i, m := range models {
		items[i] = modelToResponse(m)
	}
	var meta api.TrustMeta
	for _, m := range models {
		if m.Meta.ConfirmedAt.After(meta.ConfirmedAt) {
			meta = m.Meta
		}
	}
	return api.OK(c, items, meta)
}

func (h *Handlers) recommendCapabilityBased(c *fiber.Ctx, models []ModelRow, filter RecommendFilter) error {
	uc := intelligence.UseCase(filter.UseCase)
	pri := intelligence.Priority(filter.Priority)
	if pri == "" {
		pri = intelligence.PriorityBalanced
	}

	ids := make([]int, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	capMap, err := h.store.GetCapabilityScoresForModels(c.Context(), ids)
	if err != nil {
		log.Error().Err(err).Msg("recommend: failed to fetch capability scores")
		return api.NewInternalError("failed to retrieve capability scores")
	}

	type sm struct {
		row    ModelRow
		cs     float64
		scores map[string]float64
		caps   []intelligence.CapabilityScore
	}
	scored := make([]sm, len(models))
	for i, m := range models {
		caps := capMap[m.ID]
		cs := intelligence.ScoreModel(m.ID, uc, pri, caps)
		ds := make(map[string]float64)
		for _, cap := range caps {
			if cap.Score != nil {
				ds[cap.Dimension] = *cap.Score
			}
		}
		scored[i] = sm{row: m, cs: cs, scores: ds, caps: caps}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].cs != scored[j].cs {
			return scored[i].cs > scored[j].cs
		}
		return scored[i].row.PriceInput < scored[j].row.PriceInput
	})

	var warnings []string
	if filter.RequiresTools {
		var f []sm
		for _, s := range scored {
			if s.scores["tool_use"] >= 50 {
				f = append(f, s)
			}
		}
		if len(f) == 0 {
			warnings = append(warnings, "No models met the requires_tools threshold; showing all candidates")
		} else {
			scored = f
		}
	}
	if filter.RequiresStructuredOutput {
		var f []sm
		for _, s := range scored {
			if s.scores["structured_output"] >= 50 {
				f = append(f, s)
			}
		}
		if len(f) == 0 {
			warnings = append(warnings, "No models met the requires_structured_output threshold; showing all candidates")
		} else {
			scored = f
		}
	}

	for _, dim := range intelligence.PrimaryDimensions(uc) {
		allLow := true
		for _, s := range scored {
			for _, cap := range s.caps {
				if cap.Dimension == dim && cap.BenchmarkCount >= 2 {
					allLow = false
					break
				}
			}
			if !allLow {
				break
			}
		}
		if allLow {
			warnings = append(warnings, "Limited benchmark coverage for "+dim)
		}
	}

	topK := filter.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > len(scored) {
		topK = len(scored)
	}
	scored = scored[:topK]

	items := make([]recommendModelResponse, len(scored))
	for i, s := range scored {
		rat := intelligence.GenerateRationale(s.row.Slug, s.caps, nil, uc, pri)
		items[i] = recommendModelResponse{
			modelResponse: modelToResponse(s.row),
			Scores:        s.scores,
			Rationale:     rat,
			Warnings:      warnings,
			Fallback:      s.cs == 0,
		}
	}

	var meta api.TrustMeta
	for _, s := range scored {
		if s.row.Meta.ConfirmedAt.After(meta.ConfirmedAt) {
			meta = s.row.Meta
		}
	}
	return api.OK(c, items, meta)
}
