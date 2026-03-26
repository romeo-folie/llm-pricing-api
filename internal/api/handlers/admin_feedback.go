package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/intelligence"
)

// AdminFeedbackHandler handles GET /v1/admin/feedback.
type AdminFeedbackHandler struct {
	store FeedbackStore
}

// NewAdminFeedbackHandler creates an AdminFeedbackHandler.
func NewAdminFeedbackHandler(store FeedbackStore) *AdminFeedbackHandler {
	return &AdminFeedbackHandler{store: store}
}

// adminFeedbackResponse is the response for GET /v1/admin/feedback.
type adminFeedbackResponse struct {
	UseCase          string                        `json:"use_case"`
	PeriodDays       int                           `json:"period_days"`
	TopUpvoted       []FeedbackSummary             `json:"top_upvoted"`
	TopDownvoted     []FeedbackSummary             `json:"top_downvoted"`
	EffectiveWeights map[string]map[string]float64 `json:"effective_weights"`
}

// GetAnalytics handles GET /v1/admin/feedback?use_case=coding&days=30.
func (h *AdminFeedbackHandler) GetAnalytics(c *fiber.Ctx) error {
	useCase := c.Query("use_case", "general")
	if !intelligence.ValidUseCases[intelligence.UseCase(useCase)] {
		return api.NewBadRequest("invalid use_case: " + useCase)
	}

	days, err := strconv.Atoi(c.Query("days", "30"))
	if err != nil || days < 1 || days > 365 {
		return api.NewBadRequest("days must be an integer between 1 and 365")
	}

	topUpvoted, err := h.store.TopFeedbackModels(c.Context(), useCase, days, 10, true)
	if err != nil {
		return api.NewInternalError("failed to fetch upvoted models")
	}
	if topUpvoted == nil {
		topUpvoted = []FeedbackSummary{}
	}

	topDownvoted, err := h.store.TopFeedbackModels(c.Context(), useCase, days, 10, false)
	if err != nil {
		return api.NewInternalError("failed to fetch downvoted models")
	}
	if topDownvoted == nil {
		topDownvoted = []FeedbackSummary{}
	}

	effectiveWeights, err := h.store.GetEffectiveWeights(c.Context(), useCase)
	if err != nil {
		return api.NewInternalError("failed to fetch effective weights")
	}
	// Fallback to hardcoded weights if no calibrated weights exist.
	if effectiveWeights == nil {
		effectiveWeights = make(map[string]map[string]float64)
		for pri := range intelligence.ValidPriorities {
			effectiveWeights[string(pri)] = intelligence.DimensionWeightsFor(useCase, string(pri))
		}
	}

	return api.OK(c, adminFeedbackResponse{
		UseCase:          useCase,
		PeriodDays:       days,
		TopUpvoted:       topUpvoted,
		TopDownvoted:     topDownvoted,
		EffectiveWeights: effectiveWeights,
	}, api.TrustMeta{})
}
