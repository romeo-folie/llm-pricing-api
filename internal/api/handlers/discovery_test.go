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
)

// newDiscoveryApp builds a minimal Fiber app wiring all three discovery
// endpoints. The OpenAPI spec is the compile-time embed from static/openapi.json.
func newDiscoveryApp(store handlers.Store) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	dh := handlers.NewDiscoveryHandlerForTest(store)
	app.Get("/openapi.json", dh.GetOpenAPI)
	app.Get("/.well-known/ai-plugin.json", dh.GetAIPlugin)
	app.Get("/llms.txt", dh.GetLLMsTxt)
	return app
}

// sampleContextModels returns a small slice of ContextModelRow for use in
// /llms.txt tests.
func sampleContextModels() []handlers.ContextModelRow {
	return []handlers.ContextModelRow{
		{
			ID:          1,
			Provider:    "openai",
			Slug:        "gpt-4o",
			PriceInput:  0.0000025,
			PriceOutput: 0.000010,
			Confidence:  "high",
			ConfirmedAt: time.Now().UTC(),
		},
		{
			ID:          2,
			Provider:    "anthropic",
			Slug:        "claude-3-5-sonnet",
			PriceInput:  0.000003,
			PriceOutput: 0.000015,
			Confidence:  "high",
			ConfirmedAt: time.Now().UTC(),
		},
	}
}

// --- GET /openapi.json tests --------------------------------------------

// TestGetOpenAPI_Status200 verifies that the endpoint returns 200 OK.
func TestGetOpenAPI_Status200(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestGetOpenAPI_ContentType verifies the Content-Type is application/json.
func TestGetOpenAPI_ContentType(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

// TestGetOpenAPI_ValidJSON verifies the response body is valid JSON.
func TestGetOpenAPI_ValidJSON(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

// TestGetOpenAPI_ContainsOpenAPIVersion verifies the "openapi" key is present
// and matches the OpenAPI 3.1 version prefix.
func TestGetOpenAPI_ContainsOpenAPIVersion(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	version, ok := doc["openapi"].(string)
	if !ok || !strings.HasPrefix(version, "3.1") {
		t.Errorf("openapi field: expected 3.1.x, got %v", doc["openapi"])
	}
}

// TestGetOpenAPI_ContainsAllV1Paths verifies the spec describes all required
// /v1/ endpoint paths.
func TestGetOpenAPI_ContainsAllV1Paths(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	required := []string{
		"/v1/models",
		"/v1/models/{id}",
		"/v1/models/{id}/history",
		"/v1/compare",
		"/v1/recommend",
		"/v1/providers",
		"/v1/changes",
		"/v1/webhooks",
		"/v1/webhooks/{id}",
		"/v1/context",
		"/v1/stream/changes",
	}

	for _, path := range required {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("OpenAPI spec is missing required path: %s", path)
		}
	}
}

// --- GET /.well-known/ai-plugin.json tests ------------------------------

// TestGetAIPlugin_Status200 verifies that the endpoint returns 200 OK.
func TestGetAIPlugin_Status200(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/.well-known/ai-plugin.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestGetAIPlugin_ContainsNameForModel verifies the name_for_model field is present.
func TestGetAIPlugin_ContainsNameForModel(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/.well-known/ai-plugin.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	name, ok := manifest["name_for_model"].(string)
	if !ok || name == "" {
		t.Errorf("name_for_model: expected non-empty string, got %v", manifest["name_for_model"])
	}
}

// TestGetAIPlugin_ContainsAPIURL verifies that the manifest api.url points to
// /openapi.json.
func TestGetAIPlugin_ContainsAPIURL(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/.well-known/ai-plugin.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var manifest struct {
		API struct {
			URL string `json:"url"`
		} `json:"api"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if manifest.API.URL != "/openapi.json" {
		t.Errorf("api.url: expected /openapi.json, got %q", manifest.API.URL)
	}
}

// TestGetAIPlugin_ContentType verifies the Content-Type is application/json.
func TestGetAIPlugin_ContentType(t *testing.T) {
	app := newDiscoveryApp(&mockStore{})

	req := httptest.NewRequest("GET", "/.well-known/ai-plugin.json", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: expected application/json, got %q", ct)
	}
}

// --- GET /llms.txt tests ------------------------------------------------

// TestGetLLMsTxt_Status200 verifies that the endpoint returns 200 OK.
func TestGetLLMsTxt_Status200(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return sampleContextModels(), nil
		},
	}
	app := newDiscoveryApp(store)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestGetLLMsTxt_ContentType verifies the Content-Type is text/plain.
func TestGetLLMsTxt_ContentType(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return sampleContextModels(), nil
		},
	}
	app := newDiscoveryApp(store)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type: expected text/plain, got %q", ct)
	}
}

// TestGetLLMsTxt_Format verifies each line follows the expected format:
// {provider}/{slug}: input=${price}/1M output=${price}/1M
func TestGetLLMsTxt_Format(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return sampleContextModels(), nil
		},
	}
	app := newDiscoveryApp(store)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	text := strings.TrimSpace(string(body))
	lines := strings.Split(text, "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), text)
	}

	for _, line := range lines {
		if !strings.Contains(line, "input=$") {
			t.Errorf("line %q missing 'input=$'", line)
		}
		if !strings.Contains(line, "output=$") {
			t.Errorf("line %q missing 'output=$'", line)
		}
		if !strings.Contains(line, "/1M") {
			t.Errorf("line %q missing '/1M'", line)
		}
	}
}

// TestGetLLMsTxt_NonEmpty verifies that when models exist the response body
// is non-empty.
func TestGetLLMsTxt_NonEmpty(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return sampleContextModels(), nil
		},
	}
	app := newDiscoveryApp(store)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if len(strings.TrimSpace(string(body))) == 0 {
		t.Error("expected non-empty response body")
	}
}

// TestGetLLMsTxt_ProviderAndSlug verifies the first line contains the correct
// provider/slug combination.
func TestGetLLMsTxt_ProviderAndSlug(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			return sampleContextModels(), nil
		},
	}
	app := newDiscoveryApp(store)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	text := string(body)
	if !strings.Contains(text, "openai/gpt-4o:") {
		t.Errorf("expected line containing 'openai/gpt-4o:', got:\n%s", text)
	}
}

// TestGetLLMsTxt_PriceConversion verifies that the prices in /llms.txt are
// expressed as per-million-token values (not raw per-token values).
func TestGetLLMsTxt_PriceConversion(t *testing.T) {
	store := &mockStore{
		listModelsForCtx: func(_ context.Context, _ int) ([]handlers.ContextModelRow, error) {
			// 0.0000025 per token => 2.5 per million
			return []handlers.ContextModelRow{
				{
					ID:          1,
					Provider:    "openai",
					Slug:        "gpt-4o",
					PriceInput:  0.0000025,
					PriceOutput: 0.000010,
					Confidence:  "high",
					ConfirmedAt: time.Now().UTC(),
				},
			}, nil
		},
	}
	app := newDiscoveryApp(store)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	text := string(body)
	// 0.0000025 * 1_000_000 = 2.5; formatted as "2.5000"
	if !strings.Contains(text, "input=$2.5000/1M") {
		t.Errorf("expected 'input=$2.5000/1M' in output, got:\n%s", text)
	}
	// 0.000010 * 1_000_000 = 10.0; formatted as "10.0000"
	if !strings.Contains(text, "output=$10.0000/1M") {
		t.Errorf("expected 'output=$10.0000/1M' in output, got:\n%s", text)
	}
}
