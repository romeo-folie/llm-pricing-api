package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/intelligence"
	"llm-pricing-api/internal/middleware"
	"llm-pricing-api/internal/models"
)

// newDevApp creates a Fiber app with the RFC 7807 error handler and registers
// the three Developer+ routes WITHOUT the RequireTier middleware so that tests
// can exercise handler logic directly. Tests that need to verify tier gating
// use newDevAppWithTier.
func newDevApp(store handlers.Store) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	h := handlers.New(store)
	v1 := app.Group("/v1")
	v1.Get("/models/:id/history", h.GetModelHistory)
	v1.Get("/recommend", h.Recommend)
	v1.Get("/context", h.GetContext)
	return app
}

// newDevAppWithTier creates a Fiber app that pre-seeds the "tier" local.
// RequireTier is no longer applied to these routes (all tiers can access them);
// the tier local is injected so that downstream metrics and logging still see it.
func newDevAppWithTier(store handlers.Store, tier string) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	// Inject tier into locals before the route handler runs.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalKeyTier, tier)
		return c.Next()
	})

	h := handlers.New(store)
	v1 := app.Group("/v1")
	v1.Get("/models/:id/history", h.GetModelHistory)
	v1.Get("/recommend", h.Recommend)
	v1.Get("/context", h.GetContext)
	return app
}

// sampleHistoryRow returns a HistoryRow with plausible data and no underlying_provider.
func sampleHistoryRow(offset int) handlers.HistoryRow {
	base := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(offset) * time.Hour)
	return handlers.HistoryRow{
		InputCostPerToken:  float64(3+offset) * 1e-8,
		OutputCostPerToken: float64(6+offset) * 1e-8,
		Source:             "openrouter",
		ConfirmedAt:        base,
		RecordedAt:         base.Add(time.Minute),
	}
}

// ptr returns a pointer to the given string value, for use in test data.
func ptr(s string) *string { return &s }

// sampleContextRow returns a ContextModelRow for testing.
func sampleContextRow(id int) handlers.ContextModelRow {
	return handlers.ContextModelRow{
		ID:          id,
		Provider:    "openai",
		Slug:        "openai/gpt-4",
		PriceInput:  0.00003,
		PriceOutput: 0.00006,
		Confidence:  string(models.ConfidenceHigh),
		ConfirmedAt: time.Now().UTC(),
	}
}

// ===================================================================
// GetModelHistory tests
// ===================================================================

func TestGetModelHistory_OK(t *testing.T) {
	rows := []handlers.HistoryRow{sampleHistoryRow(0), sampleHistoryRow(1)}
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, _ handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			return rows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/models/1/history")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			InputCostPerToken  float64   `json:"input_cost_per_token"`
			OutputCostPerToken float64   `json:"output_cost_per_token"`
			Source             string    `json:"source"`
			ConfirmedAt        time.Time `json:"confirmed_at"`
			RecordedAt         time.Time `json:"recorded_at"`
		} `json:"data"`
		Meta api.TrustMeta `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 2 {
		t.Errorf("expected 2 history items, got %d", len(envelope.Data))
	}
	if envelope.Data[0].Source != "openrouter" {
		t.Errorf("expected source 'openrouter', got %q", envelope.Data[0].Source)
	}
	// Verify ConfirmedAt maps to the correct field after the historyItemResponse(r)
	// direct struct type conversion. A field-order mismatch would silently swap values.
	wantConfirmedAt := rows[0].ConfirmedAt
	if !envelope.Data[0].ConfirmedAt.Equal(wantConfirmedAt) {
		t.Errorf("confirmed_at field mismatch: want %v, got %v (field-order regression in historyItemResponse?)",
			wantConfirmedAt, envelope.Data[0].ConfirmedAt)
	}
}

func TestGetModelHistory_NotFound_Returns404(t *testing.T) {
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return false, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/models/999/history")

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", status, body)
	}
}

func TestGetModelHistory_InvalidID_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/abc/history")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestGetModelHistory_ZeroID_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/0/history")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestGetModelHistory_InvalidFromDate_Returns400(t *testing.T) {
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/models/1/history?from=not-a-date")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}
}

func TestGetModelHistory_InvalidToDate_Returns400(t *testing.T) {
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/models/1/history?to=2026-13-99T00:00:00Z")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}
}

func TestGetModelHistory_DateFiltersPassedToStore(t *testing.T) {
	// When date filters are present, GetModelHistory is called twice:
	// once with the filters (for items) and once without (for TrustMeta
	// computed over the full history). Capture the first call to verify
	// the filter was forwarded correctly.
	var capturedFilter handlers.HistoryFilter
	callCount := 0
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, f handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			callCount++
			if callCount == 1 {
				capturedFilter = f
			}
			return nil, nil
		},
	}
	app := newDevApp(store)

	get(t, app, "/v1/models/1/history?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z")

	if capturedFilter.From == nil {
		t.Error("expected From to be set")
	}
	if capturedFilter.To == nil {
		t.Error("expected To to be set")
	}
	if capturedFilter.From != nil && capturedFilter.From.Year() != 2026 {
		t.Errorf("unexpected From year: %d", capturedFilter.From.Year())
	}
}

func TestGetModelHistory_InvertedDateRange_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})

	// from is after to — should be rejected before the existence check.
	status, body := get(t, app, "/v1/models/1/history?from=2026-02-01T00:00:00Z&to=2026-01-01T00:00:00Z")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for inverted date range, got %d; body: %s", status, body)
	}
}

func TestGetModelHistory_EmptyHistory_Returns200WithEmptyArray(t *testing.T) {
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, _ handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			return nil, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/models/1/history")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	// Body should be valid JSON.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("invalid JSON: %v; body: %s", err, body)
	}
}

func TestGetModelHistory_UnderlyingProvider_NullSerialisation(t *testing.T) {
	// When underlying_provider is nil the JSON field must be present as null,
	// not omitted from the response.
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, _ handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			row := sampleHistoryRow(0) // UnderlyingProvider is nil
			return []handlers.HistoryRow{row}, nil
		},
	}
	app := newDevApp(store)

	_, body := get(t, app, "/v1/models/1/history")

	var envelope struct {
		Data []struct {
			UnderlyingProvider *string `json:"underlying_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("expected at least one history item")
	}
	if envelope.Data[0].UnderlyingProvider != nil {
		t.Errorf("expected underlying_provider to be null, got %q", *envelope.Data[0].UnderlyingProvider)
	}
	// Verify the field is present (null) in the raw JSON, not absent.
	if !strings.Contains(string(body), `"underlying_provider":null`) {
		t.Errorf("underlying_provider must be serialised as null, not omitted; body: %s", body)
	}
}

func TestGetModelHistory_UnderlyingProvider_NonNullSerialisation(t *testing.T) {
	// When underlying_provider is set it must appear as a string in the response.
	prov := "together-ai"
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, _ handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			row := sampleHistoryRow(0)
			row.UnderlyingProvider = ptr(prov)
			return []handlers.HistoryRow{row}, nil
		},
	}
	app := newDevApp(store)

	_, body := get(t, app, "/v1/models/1/history")

	var envelope struct {
		Data []struct {
			UnderlyingProvider *string `json:"underlying_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("expected at least one history item")
	}
	if envelope.Data[0].UnderlyingProvider == nil {
		t.Fatal("expected underlying_provider to be non-null")
	}
	if *envelope.Data[0].UnderlyingProvider != prov {
		t.Errorf("expected underlying_provider=%q, got %q", prov, *envelope.Data[0].UnderlyingProvider)
	}
}

// TestGetModelHistory_FreeTier_CanAccess verifies that free-tier keys can
// now access the history endpoint (tier gate removed).
func TestGetModelHistory_FreeTier_CanAccess(t *testing.T) {
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, _ handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			return []handlers.HistoryRow{sampleHistoryRow(0)}, nil
		},
	}
	app := newDevAppWithTier(store, middleware.TierFree)

	req := httptest.NewRequest("GET", "/v1/models/1/history", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == fiber.StatusForbidden {
		t.Fatal("free-tier should not receive 403 — tier gate has been removed")
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ===================================================================
// Recommend tests
// ===================================================================

func TestRecommend_OK_OrderedByPriceAsc(t *testing.T) {
	// Return models in price-ascending order to mimic SQL ORDER BY.
	ctx1, ctx2 := 8192, 16384
	rows := []handlers.ModelRow{
		{ID: 1, Provider: "openai", Name: "cheap", Slug: "openai/cheap", Modality: "text", ContextWindow: &ctx1, PriceInput: 0.000001, PriceOutput: 0.000002, Meta: api.TrustMeta{Confidence: models.ConfidenceMedium}},
		{ID: 2, Provider: "openai", Name: "pricey", Slug: "openai/pricey", Modality: "text", ContextWindow: &ctx2, PriceInput: 0.000050, PriceOutput: 0.000100, Meta: api.TrustMeta{Confidence: models.ConfidenceLow}},
	}
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return rows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/recommend")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			ID         int     `json:"id"`
			PriceInput float64 `json:"price_input"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(envelope.Data))
	}
	if envelope.Data[0].PriceInput > envelope.Data[1].PriceInput {
		t.Errorf("models not ordered by price_input asc: %v > %v",
			envelope.Data[0].PriceInput, envelope.Data[1].PriceInput)
	}
}

func TestRecommend_TaskModalityMapping_Summarisation(t *testing.T) {
	ctx := 8192
	allRows := []handlers.ModelRow{
		{ID: 1, Provider: "openai", Modality: "text", PriceInput: 0.000001, ContextWindow: &ctx, Meta: api.TrustMeta{}},
		{ID: 2, Provider: "openai", Modality: "embedding", PriceInput: 0.000002, ContextWindow: &ctx, Meta: api.TrustMeta{}},
		{ID: 3, Provider: "openai", Modality: "multimodal", PriceInput: 0.000003, ContextWindow: &ctx, Meta: api.TrustMeta{}},
	}
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return allRows, nil
		},
	}
	app := newDevApp(store)

	// "summarisation" → "text" modality
	status, body := get(t, app, "/v1/recommend?task=summarisation")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 1 {
		t.Errorf("expected 1 text model, got %d; task=summarisation should filter to modality=text", len(envelope.Data))
	}
	if len(envelope.Data) > 0 && envelope.Data[0].ID != 1 {
		t.Errorf("expected model id=1 (text modality), got id=%d", envelope.Data[0].ID)
	}
}

func TestRecommend_TaskModalityMapping_Vision(t *testing.T) {
	ctx := 8192
	allRows := []handlers.ModelRow{
		{ID: 1, Provider: "openai", Modality: "text", PriceInput: 0.000001, ContextWindow: &ctx, Meta: api.TrustMeta{}},
		{ID: 2, Provider: "openai", Modality: "multimodal", PriceInput: 0.000002, ContextWindow: &ctx, Meta: api.TrustMeta{}},
	}
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return allRows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/recommend?task=vision")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	var envelope struct {
		Data []struct{ ID int `json:"id"` } `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].ID != 2 {
		t.Errorf("expected model id=2 (multimodal), got %v", envelope.Data)
	}
}

func TestRecommend_TaskModalityMapping_Embedding(t *testing.T) {
	ctx := 8192
	allRows := []handlers.ModelRow{
		{ID: 1, Provider: "openai", Modality: "text", PriceInput: 0.000001, ContextWindow: &ctx, Meta: api.TrustMeta{}},
		{ID: 2, Provider: "openai", Modality: "embedding", PriceInput: 0.000002, ContextWindow: &ctx, Meta: api.TrustMeta{}},
	}
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return allRows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/recommend?task=embedding")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	var envelope struct {
		Data []struct{ ID int `json:"id"` } `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].ID != 2 {
		t.Errorf("expected model id=2 (embedding), got %v", envelope.Data)
	}
}

func TestRecommend_UnknownTask_NoModalityFilter(t *testing.T) {
	ctx := 8192
	allRows := []handlers.ModelRow{
		{ID: 1, Provider: "openai", Modality: "text", PriceInput: 0.000001, ContextWindow: &ctx, Meta: api.TrustMeta{}},
		{ID: 2, Provider: "openai", Modality: "multimodal", PriceInput: 0.000002, ContextWindow: &ctx, Meta: api.TrustMeta{}},
	}
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return allRows, nil
		},
	}
	app := newDevApp(store)

	// Unknown task → no modality filter applied → all rows returned.
	status, body := get(t, app, "/v1/recommend?task=unknowntask")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	var envelope struct {
		Data []struct{ ID int `json:"id"` } `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 2 {
		t.Errorf("expected 2 models for unknown task, got %d", len(envelope.Data))
	}
}

func TestRecommend_InvalidContext_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})

	status, _ := get(t, app, "/v1/recommend?context=abc")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestRecommend_NegativeContext_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})

	status, _ := get(t, app, "/v1/recommend?context=-1")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestRecommend_InvalidMaxPriceInput_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})

	status, _ := get(t, app, "/v1/recommend?max_price_input=cheap")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestRecommend_FiltersPassedToStore(t *testing.T) {
	var capturedFilter handlers.RecommendFilter
	store := &mockStore{
		recommendModels: func(_ context.Context, f handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			capturedFilter = f
			return nil, nil
		},
	}
	app := newDevApp(store)

	get(t, app, "/v1/recommend?context=4096&max_price_input=10.5")

	if capturedFilter.ContextSize == nil || *capturedFilter.ContextSize != 4096 {
		t.Errorf("ContextSize: expected 4096, got %v", capturedFilter.ContextSize)
	}
	if capturedFilter.MaxPriceInput == nil || *capturedFilter.MaxPriceInput != 10.5 {
		t.Errorf("MaxPriceInput: expected 10.5, got %v", capturedFilter.MaxPriceInput)
	}
}

// TestRecommend_FreeTier_CanAccess verifies that free-tier keys can
// access the recommend endpoint (tier gate removed).
func TestRecommend_FreeTier_CanAccess(t *testing.T) {
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{}, nil
		},
	}
	app := newDevAppWithTier(store, middleware.TierFree)

	req := httptest.NewRequest("GET", "/v1/recommend", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == fiber.StatusForbidden {
		t.Fatal("free-tier should not receive 403 — tier gate has been removed")
	}
}

// ===================================================================
// Capability-based Recommend tests (UC Phase 1)
// ===================================================================

func capabilityRecommendStore() *mockStore {
	ctx := 128000
	rows := []handlers.ModelRow{
		{ID: 1, Provider: "openai", Name: "model-a", Slug: "openai/model-a", Modality: "text", ContextWindow: &ctx, PriceInput: 0.000010, PriceOutput: 0.000020, Meta: api.TrustMeta{}},
		{ID: 2, Provider: "anthropic", Name: "model-b", Slug: "anthropic/model-b", Modality: "text", ContextWindow: &ctx, PriceInput: 0.000005, PriceOutput: 0.000010, Meta: api.TrustMeta{}},
	}
	financeScore, qualityScore := 88.0, 82.0
	codingScore, reasoningScore := 40.0, 45.0
	financeScoreB, qualityScoreB := 55.0, 90.0
	codingScoreB, reasoningScoreB := 92.0, 88.0
	return &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return rows, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			return map[int][]intelligence.CapabilityScore{
				1: {
					{ModelID: 1, Dimension: "finance", Score: &financeScore, Confidence: "high", BenchmarkCount: 2, Freshness: "fresh"},
					{ModelID: 1, Dimension: "quality", Score: &qualityScore, Confidence: "high", BenchmarkCount: 2, Freshness: "fresh"},
					{ModelID: 1, Dimension: "coding", Score: &codingScore, Confidence: "medium", BenchmarkCount: 1, Freshness: "fresh"},
					{ModelID: 1, Dimension: "reasoning", Score: &reasoningScore, Confidence: "medium", BenchmarkCount: 1, Freshness: "fresh"},
				},
				2: {
					{ModelID: 2, Dimension: "finance", Score: &financeScoreB, Confidence: "medium", BenchmarkCount: 1, Freshness: "fresh"},
					{ModelID: 2, Dimension: "quality", Score: &qualityScoreB, Confidence: "high", BenchmarkCount: 2, Freshness: "fresh"},
					{ModelID: 2, Dimension: "coding", Score: &codingScoreB, Confidence: "high", BenchmarkCount: 2, Freshness: "fresh"},
					{ModelID: 2, Dimension: "reasoning", Score: &reasoningScoreB, Confidence: "high", BenchmarkCount: 2, Freshness: "fresh"},
				},
			}, nil
		},
	}
}

func TestRecommend_UseCase_RankedByCapabilityScore(t *testing.T) {
	store := capabilityRecommendStore()
	app := newDevApp(store)
	status, body := get(t, app, "/v1/recommend?use_case=finance&priority=quality")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	// Capability-aware responses use recommendEnvelope: data.items is the ranked list.
	var envelope struct {
		Data struct {
			Items []struct {
				ID        int                `json:"id"`
				Scores    map[string]float64 `json:"scores"`
				Rationale string             `json:"rationale"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data.Items) != 2 {
		t.Fatalf("expected 2 models, got %d", len(envelope.Data.Items))
	}
	if envelope.Data.Items[0].ID != 1 {
		t.Errorf("expected model 1 ranked first for finance+quality, got id=%d", envelope.Data.Items[0].ID)
	}
	if envelope.Data.Items[0].Rationale == "" {
		t.Error("expected non-empty rationale")
	}
	if len(envelope.Data.Items[0].Scores) == 0 {
		t.Error("expected non-empty scores map")
	}
}

func TestRecommend_NoUseCase_BackwardCompatible(t *testing.T) {
	ctx := 8192
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{
				{ID: 1, Provider: "openai", Name: "cheap", Slug: "openai/cheap", Modality: "text", ContextWindow: &ctx, PriceInput: 0.000001, Meta: api.TrustMeta{Confidence: models.ConfidenceMedium}},
			}, nil
		},
	}
	app := newDevApp(store)
	status, body := get(t, app, "/v1/recommend")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	var envelope struct{ Data []json.RawMessage `json:"data"` }
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var item map[string]any
	if err := json.Unmarshal(envelope.Data[0], &item); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	if _, hasScores := item["scores"]; hasScores {
		t.Error("backward-compat response should not include 'scores' field")
	}
}

func TestRecommend_InvalidUseCase_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})
	status, _ := get(t, app, "/v1/recommend?use_case=invalid")
	if status != fiber.StatusBadRequest {
		t.Errorf("expected 400 for invalid use_case, got %d", status)
	}
}

func TestRecommend_InvalidPriority_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})
	status, _ := get(t, app, "/v1/recommend?use_case=coding&priority=invalid")
	if status != fiber.StatusBadRequest {
		t.Errorf("expected 400 for invalid priority, got %d", status)
	}
}

func TestRecommend_InvalidTopK_Returns400(t *testing.T) {
	app := newDevApp(&mockStore{})
	status, _ := get(t, app, "/v1/recommend?use_case=coding&top_k=0")
	if status != fiber.StatusBadRequest {
		t.Errorf("expected 400 for top_k=0, got %d", status)
	}
	status, _ = get(t, app, "/v1/recommend?use_case=coding&top_k=25")
	if status != fiber.StatusBadRequest {
		t.Errorf("expected 400 for top_k=25, got %d", status)
	}
}

func TestRecommend_FallbackFlag_WhenNoScores(t *testing.T) {
	ctx := 8192
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{
				{ID: 1, Provider: "openai", Name: "no-scores", Slug: "openai/no-scores", Modality: "text", ContextWindow: &ctx, PriceInput: 0.000001, Meta: api.TrustMeta{}},
			}, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			return map[int][]intelligence.CapabilityScore{}, nil
		},
	}
	app := newDevApp(store)
	status, body := get(t, app, "/v1/recommend?use_case=coding")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	var envelope struct {
		Data struct {
			Items []struct{ Fallback bool `json:"fallback"` } `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Items) != 1 {
		t.Fatalf("expected 1 model, got %d", len(envelope.Data.Items))
	}
	if !envelope.Data.Items[0].Fallback {
		t.Error("expected fallback=true when no capability scores exist")
	}
}

// TestRecommend_ScoredModelsRankBeforeZeroScoreModels verifies that models
// with real benchmark data always appear before zero-score models even when
// the zero-score models are cheaper. This prevents Fireworks/ultra-cheap
// providers from dominating the top pick slot purely due to price.
func TestRecommend_ScoredModelsRankBeforeZeroScoreModels(t *testing.T) {
	ctx := 8192
	score80 := float64(80)
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{
				// Cheapest model but zero benchmark data — should NOT win top pick
				{ID: 1, Provider: "fireworks", Name: "ultra-cheap", Slug: "fireworks/ultra-cheap", Modality: "text", ContextWindow: &ctx, PriceInput: 0.0000001, Meta: api.TrustMeta{}},
				// More expensive but has real benchmark scores — should rank first
				{ID: 2, Provider: "openai", Name: "gpt-4o", Slug: "openai/gpt-4o", Modality: "text", ContextWindow: &ctx, PriceInput: 0.000005, Meta: api.TrustMeta{}},
			}, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, ids []int) (map[int][]intelligence.CapabilityScore, error) {
			return map[int][]intelligence.CapabilityScore{
				1: {}, // no scores — fireworks model falls back to price
				2: {
					{Dimension: "coding", Score: &score80, BenchmarkCount: 3},
					{Dimension: "reasoning", Score: &score80, BenchmarkCount: 3},
				},
			}, nil
		},
	}
	app := newDevApp(store)
	status, body := get(t, app, "/v1/recommend?use_case=coding")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	var envelope struct {
		Data struct {
			Items []struct {
				Slug     string `json:"slug"`
				Fallback bool   `json:"fallback"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Items) < 2 {
		t.Fatalf("expected 2 items, got %d", len(envelope.Data.Items))
	}
	if envelope.Data.Items[0].Slug != "openai/gpt-4o" {
		t.Errorf("top pick should be the scored model, got %s", envelope.Data.Items[0].Slug)
	}
	if envelope.Data.Items[0].Fallback {
		t.Error("top pick (scored model) should not have fallback=true")
	}
	if !envelope.Data.Items[1].Fallback {
		t.Error("second model (zero-score) should have fallback=true")
	}
}

// ===================================================================
// GetContext tests
// ===================================================================

func TestGetContext_OK(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, limit int) ([]handlers.ContextModelRow, error) {
			rows := make([]handlers.ContextModelRow, 10)
			for i := range rows {
				rows[i] = sampleContextRow(i + 1)
			}
			return rows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Models []struct {
				ID          int     `json:"id"`
				Provider    string  `json:"provider"`
				Slug        string  `json:"slug"`
				PriceInput  float64 `json:"price_input"`
				PriceOutput float64 `json:"price_output"`
				Confidence  string  `json:"confidence"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data.Models) != 10 {
		t.Errorf("expected 10 models, got %d", len(envelope.Data.Models))
	}
}

func TestGetContext_ResponseSizeWithin2100Tokens(t *testing.T) {
	// Generate 50 models — the max — and verify the response fits within budget.
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, limit int) ([]handlers.ContextModelRow, error) {
			rows := make([]handlers.ContextModelRow, limit)
			for i := range rows {
				rows[i] = handlers.ContextModelRow{
					ID:          i + 1,
					Provider:    "openai",
					Slug:        "openai/gpt-4",
					PriceInput:  float64(i+1) * 0.000001,
					PriceOutput: float64(i+1) * 0.000002,
					Confidence:  "high",
					ConfirmedAt: time.Now().UTC(),
				}
			}
			return rows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	// Extract just the data portion for the token estimate; the full envelope
	// body is what matters for the API consumer.
	approxTokens := (len(body) + 3) / 4
	if approxTokens > 2100 {
		t.Errorf("response exceeds 2100-token budget: ~%d tokens (%d bytes)", approxTokens, len(body))
	}
	t.Logf("/v1/context response: %d bytes, ~%d tokens", len(body), approxTokens)
}

func TestGetContext_TrimsModelsToFitTokenBudget(t *testing.T) {
	// Return 50 models with very long slugs to force trimming.
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, limit int) ([]handlers.ContextModelRow, error) {
			rows := make([]handlers.ContextModelRow, limit)
			for i := range rows {
				rows[i] = handlers.ContextModelRow{
					ID:       i + 1,
					Provider: "anthropic-very-long-provider-name-to-inflate-size",
					// Long slug inflates the per-row byte count so the trim loop triggers.
					Slug:        "anthropic/claude-3-5-sonnet-20241022-with-extra-metadata-to-make-row-large-0000000001",
					PriceInput:  float64(i+1) * 0.0000123456789,
					PriceOutput: float64(i+1) * 0.0000987654321,
					Confidence:  "medium",
					ConfirmedAt: time.Now().UTC(),
				}
			}
			return rows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	approxTokens := (len(body) + 3) / 4
	if approxTokens > 2100 {
		t.Errorf("trimmed response still exceeds 2100-token budget: ~%d tokens (%d bytes)",
			approxTokens, len(body))
	}

	// Verify some models were returned (at least 1).
	var envelope struct {
		Data struct {
			Models []json.RawMessage `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data.Models) == 0 {
		t.Error("expected at least 1 model in trimmed response")
	}
	t.Logf("trimmed to %d models, %d bytes, ~%d tokens",
		len(envelope.Data.Models), len(body), approxTokens)
}

func TestGetContext_EmptyStore_Returns200(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return nil, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	// Verify JSON is valid.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("invalid JSON: %v; body: %s", err, body)
	}
}

// TestGetContext_FreeTier_CanAccess verifies that free-tier keys can
// access the context endpoint (tier gate removed).
func TestGetContext_FreeTier_CanAccess(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return []handlers.ContextModelRow{sampleContextRow(1)}, nil
		},
	}
	app := newDevAppWithTier(store, middleware.TierFree)

	req := httptest.NewRequest("GET", "/v1/context", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == fiber.StatusForbidden {
		t.Fatal("free-tier should not receive 403 — tier gate has been removed")
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ===================================================================
// GetContext enhanced tests (markdown format + metadata fields)
// ===================================================================

func TestGetContext_MarkdownFormat(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, limit int) ([]handlers.ContextModelRow, error) {
			rows := make([]handlers.ContextModelRow, 3)
			for i := range rows {
				rows[i] = sampleContextRow(i + 1)
			}
			return rows, nil
		},
	}
	app := newDevApp(store)

	req := httptest.NewRequest("GET", "/v1/context?format=markdown", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" || (ct != "text/markdown; charset=utf-8" && ct != "text/markdown") {
		t.Errorf("expected Content-Type text/markdown, got %q", ct)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr,"| Model |") {
		t.Error("expected markdown table header '| Model |'")
	}
	if !strings.Contains(bodyStr,"| Provider |") {
		t.Error("expected markdown table header '| Provider |'")
	}
	if !strings.Contains(bodyStr,"| Input $/1M |") {
		t.Error("expected markdown table header '| Input $/1M |'")
	}
}

func TestGetContext_MarkdownFormat_HasMetadataSummary(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return []handlers.ContextModelRow{sampleContextRow(1)}, nil
		},
	}
	app := newDevApp(store)

	req := httptest.NewRequest("GET", "/v1/context?format=markdown", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr := string(body)
	if !strings.Contains(bodyStr,"> Models:") {
		t.Errorf("expected metadata summary line '> Models:', got body: %s", bodyStr)
	}
	if !strings.Contains(bodyStr,"Estimated tokens:") {
		t.Error("expected 'Estimated tokens:' in markdown header")
	}
}

func TestGetContext_MetadataFields_TokenCount(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, limit int) ([]handlers.ContextModelRow, error) {
			rows := make([]handlers.ContextModelRow, 5)
			for i := range rows {
				rows[i] = sampleContextRow(i + 1)
			}
			return rows, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Models     []json.RawMessage `json:"models"`
			TokenCount int               `json:"token_count"`
			ModelCount int               `json:"model_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.TokenCount <= 0 {
		t.Errorf("expected token_count > 0, got %d", envelope.Data.TokenCount)
	}
	if envelope.Data.ModelCount != 5 {
		t.Errorf("expected model_count=5, got %d", envelope.Data.ModelCount)
	}
}

func TestGetContext_MetadataFields_ModelCount(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return []handlers.ContextModelRow{sampleContextRow(1), sampleContextRow(2)}, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var envelope struct {
		Data struct {
			ModelCount int `json:"model_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.ModelCount != 2 {
		t.Errorf("expected model_count=2, got %d", envelope.Data.ModelCount)
	}
}

func TestGetContext_JSONFormatUnchanged(t *testing.T) {
	// Verify existing JSON format is not broken by the new fields.
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, limit int) ([]handlers.ContextModelRow, error) {
			return []handlers.ContextModelRow{sampleContextRow(1)}, nil
		},
	}
	app := newDevApp(store)

	status, body := get(t, app, "/v1/context")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	// JSON envelope is still present.
	var envelope struct {
		Data struct {
			Models []struct {
				ID int `json:"id"`
			} `json:"models"`
		} `json:"data"`
		Meta json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data.Models) != 1 {
		t.Errorf("expected 1 model in JSON response, got %d", len(envelope.Data.Models))
	}
}

// DeveloperTier key should pass RequireTier on Developer+ endpoints.
func TestDevEndpoints_DeveloperTier_Returns200(t *testing.T) {
	store := &mockStore{
		modelExists: func(_ context.Context, _ int) (bool, error) {
			return true, nil
		},
		getModelHistory: func(_ context.Context, _ int, _ handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
			return []handlers.HistoryRow{sampleHistoryRow(0)}, nil
		},
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{sampleModel(1)}, nil
		},
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return []handlers.ContextModelRow{sampleContextRow(1)}, nil
		},
	}
	app := newDevAppWithTier(store, middleware.TierDeveloper)

	for _, path := range []string{"/v1/models/1/history", "/v1/recommend", "/v1/context"} {
		status, body := get(t, app, path)
		if status != fiber.StatusOK {
			t.Errorf("path %s: expected 200, got %d; body: %s", path, status, body)
		}
	}
}
