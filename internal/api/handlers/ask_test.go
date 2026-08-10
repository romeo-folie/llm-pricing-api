package handlers_test

import (
	"bytes"
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
)

// --- test app helpers ---------------------------------------------------

// newAskApp creates a Fiber app with POST /v1/ask wired without tier middleware
// (for testing handler logic directly).
func newAskApp(store handlers.Store) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	ask, err := handlers.NewAskHandler(store)
	if err != nil {
		panic("newAskApp: " + err.Error())
	}
	v1 := app.Group("/v1")
	v1.Post("/ask", ask.Ask)
	return app
}

// newAskAppWithTier injects a tier into Fiber locals before routing. /v1/ask is
// not tier-gated; the local is seeded so downstream metrics and logging see it.
func newAskAppWithTier(store handlers.Store, tier string) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalKeyTier, tier)
		return c.Next()
	})
	ask, err := handlers.NewAskHandler(store)
	if err != nil {
		panic("newAskAppWithTier: " + err.Error())
	}
	v1 := app.Group("/v1")
	v1.Post("/ask", ask.Ask)
	return app
}

// post performs a POST request with a JSON body and returns status + body.
func post(t *testing.T, app *fiber.App, path string, body any) (int, []byte) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, rawBody
}

// askQuery posts {"query": q} and returns status + body.
func askQuery(t *testing.T, app *fiber.App, q string) (int, []byte) {
	t.Helper()
	return post(t, app, "/v1/ask", map[string]string{"query": q})
}

// decodeAskResponse parses the response body into an envelope.
func decodeAskResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal ask response: %v; body: %s", err, body)
	}
	return env
}

// getDataField extracts the "data" object from an ask envelope.
func getDataField(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object; envelope: %v", env)
	}
	return data
}

// emptyStore returns a mockStore with a no-op recommend implementation.
func emptyRecommendStore() *mockStore {
	return &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return nil, nil
		},
	}
}

// sampleRecommendStore returns a mockStore with a one-model recommend result.
func sampleRecommendStore() *mockStore {
	ctx := 8192
	return &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{{
				ID:            1,
				Provider:      "openai",
				Name:          "cheap-model",
				Slug:          "openai/cheap-model",
				Modality:      "text",
				ContextWindow: &ctx,
				PriceInput:    0.000001,
				PriceOutput:   0.000002,
				ConfirmedAt:   time.Now().UTC(),
				Source:        "openrouter",
				Meta:          api.TrustMeta{},
			}}, nil
		},
	}
}

// ===================================================================
// Validation tests
// ===================================================================

func TestAsk_EmptyQuery_Returns400(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, _ := askQuery(t, app, "")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", status)
	}
}

func TestAsk_QueryTooLong_Returns400(t *testing.T) {
	app := newAskApp(&mockStore{})
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	status, _ := askQuery(t, app, string(long))
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for query > 500 chars, got %d", status)
	}
}

func TestAsk_QueryAtMaxLength_OK(t *testing.T) {
	app := newAskApp(emptyRecommendStore())
	at500 := make([]byte, 500)
	for i := range at500 {
		at500[i] = 'a'
	}
	// A query of 500 'a's won't match any intent → 400 with suggestions.
	// We just verify it is not rejected for length.
	status, body := askQuery(t, app, string(at500))
	// Should get 400 (no intent matched), NOT 400 for length.
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 (no intent), got %d; body: %s", status, body)
	}
	// Verify it's a "no intent" error (has suggestions), not a length error.
	var prob struct {
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(body, &prob); err != nil {
		t.Fatalf("unmarshal problem: %v; body: %s", err, body)
	}
	if _, ok := prob.Extensions["suggestions"]; !ok {
		t.Errorf("expected extensions.suggestions (no-intent error), got body: %s", body)
	}
}

func TestAsk_NoIntent_Returns400WithSuggestions(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "foobar baz quux")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for unrecognised query, got %d", status)
	}

	var prob struct {
		Status     int            `json:"status"`
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(body, &prob); err != nil {
		t.Fatalf("unmarshal problem: %v; body: %s", err, body)
	}
	if prob.Status != 400 {
		t.Errorf("expected status=400, got %d", prob.Status)
	}
	if prob.Extensions == nil {
		t.Fatal("expected extensions to contain suggestions")
	}
	sugg, ok := prob.Extensions["suggestions"]
	if !ok {
		t.Fatal("expected extensions.suggestions field")
	}
	// suggestions should be a non-empty array.
	if arr, ok := sugg.([]any); !ok || len(arr) == 0 {
		t.Errorf("expected non-empty suggestions array, got %v", sugg)
	}
}

func TestAsk_InvalidJSON_Returns400(t *testing.T) {
	app := newAskApp(&mockStore{})
	req := httptest.NewRequest("POST", "/v1/ask", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

// /v1/ask is not tier-gated — a free-tier key reaches it exactly like any other.
func TestAsk_FreeTier_Returns200(t *testing.T) {
	app := newAskAppWithTier(emptyRecommendStore(), middleware.TierFree)
	status, body := askQuery(t, app, "recommend cheapest model")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for free tier, got %d; body: %s", status, body)
	}
}

func TestAsk_DeveloperTier_Returns200(t *testing.T) {
	app := newAskAppWithTier(emptyRecommendStore(), middleware.TierDeveloper)
	status, body := askQuery(t, app, "recommend cheapest model")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for developer tier, got %d; body: %s", status, body)
	}
}

// ===================================================================
// PRICE intent tests
// ===================================================================

func TestAsk_Price_CostOf(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "cost of gpt-4o")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_PriceOf(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price of claude sonnet")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_HowMuch(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "how much does gemini flash cost")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_PricingFor(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "pricing for deepseek")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_Costs(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "gpt-4o-mini costs how much")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_WhatDoesItCost(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "what does mistral large cost per token")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_WhatIsPrice(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "what is llama 3 price")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

func TestAsk_Price_PricedAt(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "how is o1 priced at currently")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentPrice {
		t.Errorf("expected intent=price, got %v", data["intent"])
	}
}

// ===================================================================
// COMPARE intent tests
// ===================================================================

func TestAsk_Compare_Vs(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "gpt-4o vs claude sonnet")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare, got %v", data["intent"])
	}
}

func TestAsk_Compare_Versus(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "gemini flash versus gpt4o mini")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare, got %v", data["intent"])
	}
}

func TestAsk_Compare_Compare(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "compare claude opus and gpt4")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare, got %v", data["intent"])
	}
}

func TestAsk_Compare_DifferenceBetween(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "difference between deepseek and mistral")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare, got %v", data["intent"])
	}
}

func TestAsk_Compare_ComparedTo(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "how does llama 3 compare compared to gpt4o")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare, got %v", data["intent"])
	}
}

func TestAsk_Compare_ExtractsModels(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "gpt-4o vs claude sonnet pricing")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params to be an object")
	}
	models, ok := params["models"]
	if !ok {
		t.Fatal("expected inferred_params.models for compare intent")
	}
	if arr, ok := models.([]any); !ok || len(arr) == 0 {
		t.Errorf("expected non-empty models array, got %v", models)
	}
}

func TestAsk_Compare_PriceComparePhrase(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "compare prices of claude and gpt4o")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	// "compare" keyword should win over "price" keyword.
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare (wins over price), got %v", data["intent"])
	}
}

func TestAsk_Compare_ThreeModels(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "compare gpt4 vs claude vs gemini pro")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentCompare {
		t.Errorf("expected intent=compare, got %v", data["intent"])
	}
}

// ===================================================================
// HISTORY intent tests
// ===================================================================

func TestAsk_History_History(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price history of gpt-4o")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_Historical(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "historical prices for claude")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_Changed(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "has gpt4o pricing changed recently")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_Trend(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price trend for gemini over the last few months")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_PriceDrops(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "any price drops for deepseek this month")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_WentUp(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "has claude opus price went up this year")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_OverTime(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "how has mistral pricing changed over time")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
}

func TestAsk_History_LastWeek(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "gpt4o price changes last week")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentHistory {
		t.Errorf("expected intent=history, got %v", data["intent"])
	}
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params object")
	}
	if days, ok := params["days"]; !ok || days != float64(7) {
		t.Errorf("expected days=7 for 'last week', got %v", days)
	}
}

func TestAsk_History_LastMonth(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "show me claude price history last month")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, _ := data["inferred_params"].(map[string]any)
	if days, ok := params["days"]; !ok || days != float64(30) {
		t.Errorf("expected days=30 for 'last month', got %v", days)
	}
}

func TestAsk_History_ExtractsModelAlias(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price history of gpt4o this week")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params object")
	}
	if model, ok := params["model"]; !ok || model == "" {
		t.Errorf("expected model to be extracted, got %v", model)
	}
}

// ===================================================================
// RECOMMEND intent tests
// ===================================================================

func TestAsk_Recommend_Cheapest(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "cheapest model for text generation")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_Recommend(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "recommend a model for summarization")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_BestModel(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "best model for coding under $5")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_Suggest(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "suggest a cheap model with 128k context")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_UnderDollar(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "models under $2 per million tokens")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_MostAffordable(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "most affordable model for embeddings")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_BestFor(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "what is the best for summarization under $10")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_Optimal(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "optimal model for text analysis")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if data["intent"] != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %v", data["intent"])
	}
}

func TestAsk_Recommend_ExtractsPriceConstraint(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "recommend a model under $3.50 per million tokens")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params object")
	}
	if maxPrice, ok := params["max_price"]; !ok {
		t.Error("expected max_price to be extracted")
	} else if maxPrice != float64(3.5) {
		t.Errorf("expected max_price=3.5, got %v", maxPrice)
	}
}

func TestAsk_Recommend_ExtractsContextWindow(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "suggest a model with 128k context window")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params object")
	}
	if ctxWindow, ok := params["context_window"]; !ok {
		t.Error("expected context_window to be extracted")
	} else if ctxWindow != float64(128000) {
		t.Errorf("expected context_window=128000, got %v", ctxWindow)
	}
}

func TestAsk_Recommend_ReturnsRankedModels(t *testing.T) {
	app := newAskApp(sampleRecommendStore())
	status, body := askQuery(t, app, "cheapest model for summarization")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	if _, ok := data["ranked_models"]; !ok {
		t.Error("expected ranked_models field for recommend intent")
	}
}

func TestAsk_Recommend_EmptyResults_StillOK(t *testing.T) {
	app := newAskApp(emptyRecommendStore())
	status, body := askQuery(t, app, "cheapest model for coding")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 even with empty results, got %d; body: %s", status, body)
	}
}

// ===================================================================
// Use-case routing tests (UC Phase 1)
// ===================================================================

func TestAsk_Recommend_UseCaseDetection_Coding(t *testing.T) {
	ctx := 8192
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{{
				ID: 1, Provider: "openai", Name: "model-a", Slug: "openai/model-a",
				Modality: "text", ContextWindow: &ctx,
				PriceInput: 0.000010, PriceOutput: 0.000020,
				ConfirmedAt: time.Now().UTC(), Source: "openrouter",
				Meta: api.TrustMeta{},
			}}, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			s := 85.0
			return map[int][]intelligence.CapabilityScore{
				1: {{ModelID: 1, Dimension: "coding", Score: &s, Confidence: "high", BenchmarkCount: 3, Freshness: "fresh"}},
			}, nil
		},
	}
	app := newAskApp(store)

	status, body := askQuery(t, app, "best model for coding")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	data := getDataField(t, decodeAskResponse(t, body))
	intent, _ := data["intent"].(string)
	if intent != handlers.IntentRecommend {
		t.Errorf("expected intent=recommend, got %s", intent)
	}
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params to be an object")
	}
	uc, _ := params["use_case"].(string)
	if uc != "coding" {
		t.Errorf("expected inferred_params.use_case=coding, got %s", uc)
	}
	summary, _ := data["plain_english_summary"].(string)
	if summary == "" {
		t.Error("expected non-empty plain_english_summary")
	}
}

func TestAsk_Recommend_UseCaseDetection_Finance(t *testing.T) {
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return nil, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			return map[int][]intelligence.CapabilityScore{}, nil
		},
	}
	app := newAskApp(store)

	status, body := askQuery(t, app, "best model for financial analysis")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params to be an object")
	}
	uc, _ := params["use_case"].(string)
	if uc != "finance" {
		t.Errorf("expected inferred_params.use_case=finance, got %q", uc)
	}
}

func TestAsk_Recommend_UseCaseDetection_Reasoning(t *testing.T) {
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return nil, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			return map[int][]intelligence.CapabilityScore{}, nil
		},
	}
	app := newAskApp(store)

	status, body := askQuery(t, app, "best model for reasoning and logic tasks")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params to be an object")
	}
	uc, _ := params["use_case"].(string)
	if uc != "reasoning" {
		t.Errorf("expected inferred_params.use_case=reasoning, got %q", uc)
	}
}

func TestAsk_Recommend_UseCaseDetection_Agentic(t *testing.T) {
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return nil, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			return map[int][]intelligence.CapabilityScore{}, nil
		},
	}
	app := newAskApp(store)

	status, body := askQuery(t, app, "best model for agentic tool use workflows")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, ok := data["inferred_params"].(map[string]any)
	if !ok {
		t.Fatal("expected inferred_params to be an object")
	}
	uc, _ := params["use_case"].(string)
	if uc != "agentic" {
		t.Errorf("expected inferred_params.use_case=agentic, got %q", uc)
	}
}

func TestAsk_UseCaseRouting_IncludesRationale(t *testing.T) {
	ctx := 8192
	store := &mockStore{
		recommendModels: func(_ context.Context, _ handlers.RecommendFilter) ([]handlers.ModelRow, error) {
			return []handlers.ModelRow{{
				ID: 1, Provider: "openai", Name: "model-a", Slug: "openai/model-a",
				Modality: "text", ContextWindow: &ctx,
				PriceInput: 0.000010, PriceOutput: 0.000020,
				ConfirmedAt: time.Now().UTC(), Source: "openrouter",
				Meta: api.TrustMeta{},
			}}, nil
		},
		getCapabilityScoresForModels: func(_ context.Context, _ []int) (map[int][]intelligence.CapabilityScore, error) {
			s := 88.0
			return map[int][]intelligence.CapabilityScore{
				1: {{ModelID: 1, Dimension: "coding", Score: &s, Confidence: "high", BenchmarkCount: 3, Freshness: "fresh"}},
			}, nil
		},
	}
	app := newAskApp(store)

	status, body := askQuery(t, app, "best model for coding tasks")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))

	// plain_english_summary should contain the use case and model info.
	summary, _ := data["plain_english_summary"].(string)
	if summary == "" {
		t.Error("expected non-empty plain_english_summary with rationale")
	}
	// Should mention the use case in the summary.
	if !strings.Contains(strings.ToLower(summary), "coding") {
		t.Errorf("expected summary to mention 'coding', got %q", summary)
	}
}

// ===================================================================
// Response shape tests
// ===================================================================

func TestAsk_ResponseShape_HasRequiredFields(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "cost of gpt-4o")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	env := decodeAskResponse(t, body)
	data := getDataField(t, env)

	for _, field := range []string{"intent", "inferred_params", "plain_english_summary", "meta"} {
		if _, ok := data[field]; !ok {
			t.Errorf("expected response data.%s field to be present", field)
		}
	}
}

func TestAsk_ResponseMeta_ParserIsRuleV1(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price of deepseek")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	meta, ok := data["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected data.meta to be an object")
	}
	if meta["parser"] != handlers.AskParserVersion {
		t.Errorf("expected meta.parser=%q, got %v", handlers.AskParserVersion, meta["parser"])
	}
}

func TestAsk_ResponseMeta_QueryTimeMs(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price of gemini")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	meta, ok := data["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected data.meta to be an object")
	}
	if _, ok := meta["query_time_ms"]; !ok {
		t.Error("expected meta.query_time_ms to be present")
	}
}

func TestAsk_PlainEnglishSummary_NotEmpty(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "what does gpt4o cost")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	summary, _ := data["plain_english_summary"].(string)
	if summary == "" {
		t.Error("expected non-empty plain_english_summary")
	}
}

// ===================================================================
// Alias normalisation tests
// ===================================================================

func TestAsk_Alias_GPT4oNormalised(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "cost of gpt4o")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, _ := data["inferred_params"].(map[string]any)
	if model, ok := params["model"].(string); !ok || model == "" {
		t.Errorf("expected model alias to be resolved, got %v", params["model"])
	}
}

func TestAsk_Alias_ClaudeSonnetNormalised(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "price of claude sonnet")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, _ := data["inferred_params"].(map[string]any)
	model, _ := params["model"].(string)
	if model == "" {
		t.Errorf("expected claude sonnet alias to resolve, got empty")
	}
}

func TestAsk_Alias_GeminiFlashNormalised(t *testing.T) {
	app := newAskApp(&mockStore{})
	status, body := askQuery(t, app, "how much does gemini flash cost")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, _ := data["inferred_params"].(map[string]any)
	model, _ := params["model"].(string)
	if model == "" {
		t.Errorf("expected gemini flash alias to resolve")
	}
}

func TestAsk_Alias_UnknownModel_NoModelInParams(t *testing.T) {
	app := newAskApp(&mockStore{})
	// "exoticmodel" is not in the alias map.
	status, body := askQuery(t, app, "what is the cost of exoticmodel123")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	data := getDataField(t, decodeAskResponse(t, body))
	params, _ := data["inferred_params"].(map[string]any)
	// model may be absent or empty — both are acceptable.
	if model, ok := params["model"]; ok && model != "" {
		t.Errorf("expected no model for unknown alias, got %v", model)
	}
}

