package handlers

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/intelligence"
)

const (
	// askMaxQueryLength is the hard cap on query string length.
	askMaxQueryLength = 500

	// AskParserVersion is included in every /v1/ask response meta so callers
	// can detect parser upgrades.
	AskParserVersion = "rule-v1"
)

// Intent constants for the four supported query intents.
// Exported so tests in the handlers_test package can assert without
// duplicating the string literals.
const (
	IntentPrice     = "price"
	IntentCompare   = "compare"
	IntentHistory   = "history"
	IntentRecommend = "recommend"
)

// --- compiled regexes ---------------------------------------------------

// comparePatterns match "X vs Y", "compare X and Y", "difference between X and Y".
var comparePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bvs\.?\b`),
	regexp.MustCompile(`\bversus\b`),
	regexp.MustCompile(`\bcompare\b`),
	regexp.MustCompile(`\bdifference between\b`),
	regexp.MustCompile(`\bcompared to\b`),
}

// recommendPatterns match "cheapest", "recommend", "best X for", etc.
var recommendPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bcheapest\b`),
	regexp.MustCompile(`\bcheap\b`),
	regexp.MustCompile(`\brecommend\b`),
	regexp.MustCompile(`\bsuggest(?:s|ed)?\b`),
	regexp.MustCompile(`\bbest model\b`),
	regexp.MustCompile(`\bmost affordable\b`),
	regexp.MustCompile(`\bunder \$?\d`),
	regexp.MustCompile(`\bbest for\b`),
	regexp.MustCompile(`\boptimal\b`),
}

// historyPatterns match queries about historical price data.
var historyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bhistory\b`),
	regexp.MustCompile(`\bhistorical\b`),
	regexp.MustCompile(`\bchanged\b`),
	regexp.MustCompile(`\btrend\b`),
	regexp.MustCompile(`\bprice drops?\b`),
	regexp.MustCompile(`\bwent up\b`),
	regexp.MustCompile(`\bover time\b`),
	regexp.MustCompile(`\blast month\b`),
	regexp.MustCompile(`\blast week\b`),
	regexp.MustCompile(`\bpast \d+ days?\b`),
}

// pricePatterns match direct pricing queries.
var pricePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bcost of\b`),
	regexp.MustCompile(`\bprice of\b`),
	regexp.MustCompile(`\bpricing for\b`),
	regexp.MustCompile(`\bhow much\b`),
	regexp.MustCompile(`\bwhat does .+ cost\b`),
	regexp.MustCompile(`\bwhat is .+ price\b`),
	regexp.MustCompile(`\bcosts?\b`),
	regexp.MustCompile(`\bpriced at\b`),
}

// priceConstraintRe extracts "under $N" or "under N" price constraints.
var priceConstraintRe = regexp.MustCompile(`under \$?(\d+(?:\.\d+)?)`)

// contextWindowRe extracts context window hints like "128k", "32k tokens".
// The \b after k prevents matching unit suffixes such as "128kb".
var contextWindowRe = regexp.MustCompile(`(\d+)k\b(?:\s+tokens?)?`)

// timeDaysRe extracts "X days" from history queries.
var timeDaysRe = regexp.MustCompile(`past (\d+) days?`)

// --- response types -----------------------------------------------------

// askMeta is the per-request metadata included in /v1/ask responses.
type askMeta struct {
	QueryTimeMs int64  `json:"query_time_ms"`
	Parser      string `json:"parser"`
}

// askResponse is the /v1/ask success payload.
type askResponse struct {
	Intent              string         `json:"intent"`
	InferredParams      map[string]any `json:"inferred_params"`
	Models              []modelResponse `json:"models,omitempty"`
	RankedModels        []modelResponse `json:"ranked_models,omitempty"`
	PlainEnglishSummary string         `json:"plain_english_summary"`
	Meta                askMeta        `json:"meta"`
}

// --- AskHandler ---------------------------------------------------------

// AskHandler holds dependencies for the POST /v1/ask endpoint.
type AskHandler struct {
	store        Store
	queriesTotal metric.Int64Counter
}

// NewAskHandler creates an AskHandler backed by the provided Store and
// registers the ask_queries_total OTel counter.
func NewAskHandler(store Store) (*AskHandler, error) {
	meter := otel.GetMeterProvider().Meter("llm-pricing-api")

	counter, err := meter.Int64Counter(
		"llm_pricing.ask.queries_total",
		metric.WithDescription("Total /v1/ask queries labelled by intent"),
	)
	if err != nil {
		return nil, fmt.Errorf("create ask_queries_total counter: %w", err)
	}

	return &AskHandler{store: store, queriesTotal: counter}, nil
}

// Ask handles POST /v1/ask.
// Developer+ only — enforced by RequireTier middleware at route registration.
//
// The handler parses the natural language query into one of four intents
// (price, compare, history, recommend) using a deterministic regex cascade.
// No LLM is involved; parsing is synchronous and sub-50ms.
//
// Request body: { "query": "..." }
//
// Validation:
//   - Empty query → 400
//   - Query length > 500 chars → 400
//   - Unrecognised query → 400 with suggestions
func (h *AskHandler) Ask(c *fiber.Ctx) error {
	start := time.Now()

	var body struct {
		Query string `json:"query"`
	}
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("request body must be valid JSON with a 'query' field")
	}

	query := strings.TrimSpace(body.Query)
	if query == "" {
		return api.NewBadRequest("query must not be empty")
	}
	if len(query) > askMaxQueryLength {
		return api.NewBadRequest(fmt.Sprintf("query exceeds maximum length of %d characters", askMaxQueryLength))
	}

	norm := strings.ToLower(query)

	intent, params := parseIntent(norm)
	if intent == "" {
		pd := api.NewBadRequest("query does not match any supported intent")
		pd.Extensions = map[string]any{
			"suggestions": []string{
				"What is the price of GPT-4o?",
				"Compare Claude Sonnet vs GPT-4o",
				"Show price history for Mistral Large",
				"Recommend the cheapest model for summarization",
			},
		}
		return pd
	}

	// Increment OTel counter with intent label.
	h.queriesTotal.Add(c.Context(), 1,
		metric.WithAttributes(attribute.String("intent", intent)),
	)

	// Build response (may include a DB call for recommend intent).
	resp, err := h.buildResponse(c, intent, params)
	if err != nil {
		return api.NewInternalError("failed to process query")
	}

	// Measure total handler latency (including any DB round-trip) so that
	// meta.query_time_ms reflects the true latency experienced by the caller.
	resp.Meta.QueryTimeMs = time.Since(start).Milliseconds()

	return api.OK(c, resp, api.TrustMeta{})
}

// buildResponse delegates to the store for intents that support DB lookups
// and constructs the askResponse envelope. The caller is responsible for
// setting Meta.QueryTimeMs after buildResponse returns so that the elapsed
// time includes the DB round-trip.
func (h *AskHandler) buildResponse(
	c *fiber.Ctx,
	intent string,
	params map[string]any,
) (*askResponse, error) {
	resp := &askResponse{
		Intent:         intent,
		InferredParams: params,
		Meta: askMeta{
			Parser: AskParserVersion,
		},
	}

	switch intent {
	case IntentRecommend:
		filter := RecommendFilter{}
		if v, ok := params["max_price"]; ok {
			if f, ok := v.(float64); ok {
				filter.MaxPriceInput = &f
			}
		}
		if v, ok := params["context_window"]; ok {
			if i, ok := v.(int); ok {
				filter.ContextSize = &i
			}
		}
		if v, ok := params["task"]; ok {
			if s, ok := v.(string); ok {
				filter.Task = s
			}
		}
		if v, ok := params["use_case"]; ok {
			if s, ok := v.(string); ok {
				filter.UseCase = s
				filter.Priority = "balanced"
			}
		}
		filter.TopK = 5

		models, err := h.store.RecommendModels(c.Context(), filter)
		if err != nil {
			log.Error().Err(err).Str("intent", IntentRecommend).Msg("ask: recommend query failed")
			return nil, err
		}

		// Apply task→modality in handler layer (mirrors recommend.go).
		if filter.Task != "" {
			if modality, ok := taskModalityMap[filter.Task]; ok {
				filtered := make([]ModelRow, 0, len(models))
				for _, m := range models {
					if m.Modality == modality {
						filtered = append(filtered, m)
					}
				}
				models = filtered
			}
		}

		// If use_case is set, apply capability scoring for the ask response.
		if filter.UseCase != "" {
			if err := h.buildCapabilityAskResponse(c, resp, models, filter); err != nil {
				return nil, err
			}
		} else {
			ranked := make([]modelResponse, len(models))
			for i, m := range models {
				ranked[i] = modelToResponse(m)
			}
			resp.RankedModels = ranked
			resp.PlainEnglishSummary = buildRecommendSummary(models, params)
		}

	case IntentPrice:
		var model string
		if v, ok := params["model"]; ok {
			model, _ = v.(string)
		}
		resp.PlainEnglishSummary = buildPriceSummary(model)

	case IntentCompare:
		var names []string
		if v, ok := params["models"]; ok {
			if ss, ok := v.([]string); ok {
				names = ss
			}
		}
		resp.PlainEnglishSummary = buildCompareSummary(names)

	case IntentHistory:
		var model string
		if v, ok := params["model"]; ok {
			model, _ = v.(string)
		}
		resp.PlainEnglishSummary = buildHistorySummary(model, params)
	}

	return resp, nil
}

// buildCapabilityAskResponse scores models by capability and populates the
// ask response with ranked results including rationale.
func (h *AskHandler) buildCapabilityAskResponse(
	c *fiber.Ctx,
	resp *askResponse,
	models []ModelRow,
	filter RecommendFilter,
) error {
	useCase := intelligence.UseCase(filter.UseCase)
	priority := intelligence.Priority(filter.Priority)
	if priority == "" {
		priority = intelligence.PriorityBalanced
	}

	modelIDs := make([]int, len(models))
	for i, m := range models {
		modelIDs[i] = m.ID
	}

	capScoresMap, err := h.store.GetCapabilityScoresForModels(c.Context(), modelIDs)
	if err != nil {
		log.Error().Err(err).Msg("ask: failed to fetch capability scores")
		return err
	}

	type scored struct {
		row      ModelRow
		capScore float64
		caps     []intelligence.CapabilityScore
	}
	items := make([]scored, len(models))
	for i, m := range models {
		caps := capScoresMap[m.ID]
		cs := intelligence.ScoreModel(m.ID, useCase, priority, caps)
		items[i] = scored{row: m, capScore: cs, caps: caps}
	}

	const epsilon = 1e-9
	sort.SliceStable(items, func(i, j int) bool {
		if math.Abs(items[i].capScore-items[j].capScore) > epsilon {
			return items[i].capScore > items[j].capScore
		}
		return items[i].row.PriceInput < items[j].row.PriceInput
	})

	topK := filter.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > len(items) {
		topK = len(items)
	}
	items = items[:topK]

	ranked := make([]modelResponse, len(items))
	for i, s := range items {
		ranked[i] = modelToResponse(s.row)
	}
	resp.RankedModels = ranked

	if len(items) > 0 {
		top := items[0]
		rationale := intelligence.GenerateRationale(
			top.row.Slug, top.caps, nil, useCase, priority,
		)
		resp.PlainEnglishSummary = fmt.Sprintf(
			"For %s tasks, the top recommendation is %s. %s",
			filter.UseCase, top.row.Slug, rationale,
		)
	} else {
		resp.PlainEnglishSummary = "No models matched your criteria. Try relaxing constraints."
	}

	return nil
}

// --- parser -------------------------------------------------------------

// parseIntent runs the priority-ordered regex cascade on the normalised query.
// Priority: compare > recommend > history > price.
// Returns ("", nil) if no intent is matched.
func parseIntent(norm string) (string, map[string]any) {
	// 1. compare
	for _, re := range comparePatterns {
		if re.MatchString(norm) {
			return IntentCompare, extractCompareParams(norm)
		}
	}

	// 2. recommend
	for _, re := range recommendPatterns {
		if re.MatchString(norm) {
			return IntentRecommend, extractRecommendParams(norm)
		}
	}

	// 3. history
	for _, re := range historyPatterns {
		if re.MatchString(norm) {
			return IntentHistory, extractHistoryParams(norm)
		}
	}

	// 4. price
	for _, re := range pricePatterns {
		if re.MatchString(norm) {
			return IntentPrice, extractPriceParams(norm)
		}
	}

	return "", nil
}

// extractPriceParams extracts model name(s) from a price-intent query.
func extractPriceParams(norm string) map[string]any {
	params := map[string]any{}
	if slug := resolveAlias(norm); slug != "" {
		params["model"] = slug
	}
	return params
}

// extractCompareParams extracts model names from a compare-intent query.
// Handles "X vs Y", "compare X and Y", and "compare X, Y, Z" patterns.
//
// Longest-match semantics are applied: an alias is skipped when a strictly
// longer alias that (a) contains the shorter alias as a substring and (b) also
// appears in the query supersedes it. This prevents "gpt4" from matching
// inside a query that already matches "gpt4o".
func extractCompareParams(norm string) map[string]any {
	params := map[string]any{}

	var models []string
	seen := map[string]struct{}{}
	for alias, slug := range ModelAliases {
		if !strings.Contains(norm, alias) {
			continue
		}
		// Skip if a longer alias that is a superstring of this one also matches.
		superseded := false
		for longer := range ModelAliases {
			if len(longer) > len(alias) &&
				strings.Contains(longer, alias) &&
				strings.Contains(norm, longer) {
				superseded = true
				break
			}
		}
		if superseded {
			continue
		}
		if _, ok := seen[slug]; !ok {
			seen[slug] = struct{}{}
			models = append(models, slug)
		}
	}

	if len(models) > 0 {
		sort.Strings(models)
		params["models"] = models
	}
	return params
}

// extractRecommendParams extracts price constraints, context window hints,
// and task keywords from a recommend-intent query.
func extractRecommendParams(norm string) map[string]any {
	params := map[string]any{}

	// Price constraint: "under $5", "under 5".
	if m := priceConstraintRe.FindStringSubmatch(norm); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			params["max_price"] = v
		}
	}

	// Context window: "128k", "32k tokens".
	if m := contextWindowRe.FindStringSubmatch(norm); len(m) > 1 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			params["context_window"] = v * 1000
		}
	}

	// Task keywords (match taskModalityMap keys + extras).
	taskKeywords := []string{
		"summariz", "summaris", "classification", "generation", "vision",
		"multimodal", "image", "audio", "embedding", "text",
		"coding", "code", "analysis",
	}
	for _, kw := range taskKeywords {
		if strings.Contains(norm, kw) {
			// Normalise to a taskModalityMap key where possible.
			switch {
			case strings.HasPrefix(kw, "summariz"), strings.HasPrefix(kw, "summaris"):
				params["task"] = "summarization"
			case kw == "coding" || kw == "code":
				params["task"] = "coding"
			default:
				params["task"] = kw
			}
			break
		}
	}

	// Use-case detection (longest-match first).
	for _, kw := range useCaseKeywordOrder {
		if strings.Contains(norm, kw) {
			params["use_case"] = useCaseKeywords[kw]
			break
		}
	}

	// Model preference (optional — for "best [model] for X" patterns).
	if slug := resolveAlias(norm); slug != "" {
		params["preferred_model"] = slug
	}

	return params
}

// useCaseKeywords maps natural language keywords to UseCase values for
// the /v1/ask recommend intent.
var useCaseKeywords = map[string]string{
	"finance":          "finance",
	"financial":        "finance",
	"banking":          "finance",
	"investment":       "finance",
	"coding":           "coding",
	"code":             "coding",
	"programming":      "coding",
	"developer":        "coding",
	"agentic":          "agentic",
	"ai agent":         "agentic", // "agent" alone is too broad (e.g. "real estate agent")
	"tool use":         "agentic",
	"function calling": "agentic",
	"writing code":     "coding",  // must appear before "writing" in longest-first order
	"writing":          "writing",
	"copywriting":      "writing",
	"content creation": "writing",
	"video":            "video",
	"video analysis":   "video",
	"reasoning":        "reasoning",
	"logic":            "reasoning",
	"math":             "reasoning",
	"mathematics":      "reasoning",
	"general":          "general",
	"general purpose":  "general",
}

// useCaseKeywordOrder is checked in order (longest first) to ensure
// multi-word phrases match before their substrings.
var useCaseKeywordOrder []string

func init() {
	useCaseKeywordOrder = make([]string, 0, len(useCaseKeywords))
	for kw := range useCaseKeywords {
		useCaseKeywordOrder = append(useCaseKeywordOrder, kw)
	}
	sort.Slice(useCaseKeywordOrder, func(i, j int) bool {
		return len(useCaseKeywordOrder[i]) > len(useCaseKeywordOrder[j])
	})
}

// extractHistoryParams extracts model name and optional time reference
// from a history-intent query.
func extractHistoryParams(norm string) map[string]any {
	params := map[string]any{}

	if slug := resolveAlias(norm); slug != "" {
		params["model"] = slug
	}

	// Time reference extraction.
	switch {
	case strings.Contains(norm, "this week") || strings.Contains(norm, "last week"):
		params["days"] = 7
	case strings.Contains(norm, "this month") || strings.Contains(norm, "last month"):
		params["days"] = 30
	case strings.Contains(norm, "this year") || strings.Contains(norm, "last year"):
		params["days"] = 365
	default:
		if m := timeDaysRe.FindStringSubmatch(norm); len(m) > 1 {
			if v, err := strconv.Atoi(m[1]); err == nil {
				params["days"] = v
			}
		}
	}

	return params
}

// resolveAlias finds the first matching model alias in the normalised query
// and returns its canonical slug, or "" if no alias is found.
func resolveAlias(norm string) string {
	// Try longer aliases first for more specific matches.
	best := ""
	bestLen := 0
	for alias, slug := range ModelAliases {
		if strings.Contains(norm, alias) && len(alias) > bestLen {
			best = slug
			bestLen = len(alias)
		}
	}
	return best
}

// --- summary builders ---------------------------------------------------

func buildPriceSummary(model string) string {
	if model == "" {
		return "Use the inferred_params to look up pricing for the model in question."
	}
	return fmt.Sprintf(
		"Check the current pricing for %s via GET /v1/models with a provider filter.",
		model,
	)
}

func buildCompareSummary(models []string) string {
	if len(models) == 0 {
		return "Use the inferred_params to compare the requested models via GET /v1/compare."
	}
	return fmt.Sprintf(
		"Compare %s using GET /v1/compare with their model IDs.",
		strings.Join(models, " vs "),
	)
}

func buildHistorySummary(model string, params map[string]any) string {
	if model == "" {
		return "Use the inferred_params to retrieve price history via GET /v1/models/:id/history."
	}
	if days, ok := params["days"]; ok {
		return fmt.Sprintf(
			"Price history for %s over the past %v days is available via GET /v1/models/:id/history.",
			model, days,
		)
	}
	return fmt.Sprintf(
		"Price history for %s is available via GET /v1/models/:id/history.",
		model,
	)
}

func buildRecommendSummary(models []ModelRow, params map[string]any) string {
	if len(models) == 0 {
		return "No models matched your criteria. Try relaxing the price or context window constraints."
	}
	top := models[0]
	summary := fmt.Sprintf(
		"Found %d model(s) matching your criteria. Top recommendation: %s (input: $%.4f/1M tokens).",
		len(models), top.Slug, top.PriceInput*1_000_000,
	)
	if _, ok := params["max_price"]; ok {
		summary += " Results filtered by max price constraint."
	}
	return summary
}
