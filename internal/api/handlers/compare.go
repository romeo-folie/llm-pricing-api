package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/intelligence"
)

const maxCompareModels = 5

// compareModelResponse extends modelResponse with capability scoring data
// returned by the enhanced /v1/compare endpoint.
type compareModelResponse struct {
	modelResponse
	Scores    map[string]float64 `json:"scores,omitempty"`
	Freshness map[string]string  `json:"freshness,omitempty"`
	Rationale string             `json:"rationale,omitempty"`
	Warnings  []string           `json:"warnings,omitempty"`
}

// compareEnvelope wraps compare response items together with result-level
// warnings. Passed as the `data` field to api.OK.
type compareEnvelope struct {
	Items    []compareModelResponse `json:"items"`
	Warnings []string               `json:"warnings,omitempty"`
}

// Compare handles GET /v1/compare?models=slug1,slug2[&use_case=X]
// Returns side-by-side pricing + capability scores for up to 5 model slugs.
// Accepts model slugs (strings). Falls back to integer ID lookup for
// backward compatibility.
// Returns 400 if more than 5 slugs are supplied.
// Returns 404 if any model slug is not found.
func (h *Handlers) Compare(c *fiber.Ctx) error {
	rawModels := c.Query("models")
	if rawModels == "" {
		return api.NewBadRequest("models query parameter is required")
	}

	parts := strings.Split(rawModels, ",")
	if len(parts) > maxCompareModels {
		return api.NewBadRequest("compare supports at most 5 models")
	}

	// De-duplicate slugs while preserving order.
	slugs := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, dup := seen[part]; dup {
			continue
		}
		seen[part] = struct{}{}
		slugs = append(slugs, part)
	}

	if len(slugs) == 0 {
		return api.NewBadRequest("at least one model slug is required")
	}

	models, err := h.store.CompareModelsBySlugs(c.Context(), slugs)
	if err != nil {
		if err == ErrNotFound {
			return api.NewNotFound("one or more model slugs were not found")
		}
		return api.NewInternalError("failed to compare models")
	}

	// Fetch capability scores for all models.
	ids := make([]int, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	capMap, err := h.store.GetCapabilityScoresForModels(c.Context(), ids)
	if err != nil {
		log.Error().Err(err).Msg("compare: failed to fetch capability scores")
		// Non-fatal: proceed without capability data.
		capMap = make(map[int][]intelligence.CapabilityScore)
	}

	// Parse optional use_case parameter.
	var uc intelligence.UseCase
	ucRaw := c.Query("use_case")
	if ucRaw != "" {
		uc = intelligence.UseCase(ucRaw)
		if !intelligence.ValidUseCases[uc] {
			return api.NewBadRequest("use_case must be one of: finance, coding, agentic, writing, video, reasoning, general")
		}
	}

	var warnings []string
	items := make([]compareModelResponse, len(models))
	for i, m := range models {
		caps := capMap[m.ID]

		// Build per-dimension score and freshness maps.
		ds := make(map[string]float64)
		var fs map[string]string
		for _, cap := range caps {
			if cap.Score != nil {
				ds[cap.Dimension] = *cap.Score
			}
			if cap.Freshness != "" {
				if fs == nil {
					fs = make(map[string]string)
				}
				fs[cap.Dimension] = cap.Freshness
			}
		}

		// Generate rationale only when use_case is provided.
		var rationale string
		if uc != "" {
			rationale = intelligence.GenerateRationale(m.Slug, caps, nil, uc, intelligence.PriorityBalanced)
		}

		items[i] = compareModelResponse{
			modelResponse: modelToResponse(m),
			Scores:        ds,
			Freshness:     fs,
			Rationale:     rationale,
		}
	}

	// Check benchmark coverage warnings when use_case is provided.
	if uc != "" {
		for _, dim := range intelligence.PrimaryDimensions(uc) {
			allLow := true
			for _, m := range models {
				for _, cap := range capMap[m.ID] {
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
	}

	// Aggregate meta: use the most recently confirmed model's meta.
	var meta api.TrustMeta
	for _, m := range models {
		if m.Meta.ConfirmedAt.After(meta.ConfirmedAt) {
			meta = m.Meta
		}
	}

	return api.OK(c, compareEnvelope{Items: items, Warnings: warnings}, meta)
}
