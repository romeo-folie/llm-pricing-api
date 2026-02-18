package litellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const validLiteLLMResponse = `{
  "gpt-4": {
    "input_cost_per_token": 0.00003,
    "output_cost_per_token": 0.00006,
    "max_input_tokens": 8192,
    "litellm_provider": "openai"
  },
  "anthropic/claude-3-opus-20240229": {
    "input_cost_per_token": 0.000015,
    "output_cost_per_token": 0.000075,
    "max_input_tokens": 200000,
    "litellm_provider": "anthropic"
  },
  "gemini/gemini-pro": {
    "input_cost_per_token": 0.0000005,
    "output_cost_per_token": 0.0000015,
    "max_input_tokens": 32000
  }
}`

const missingPricesLiteLLMResponse = `{
  "gpt-4": {
    "input_cost_per_token": 0.00003,
    "output_cost_per_token": 0.00006,
    "max_tokens": 8192,
    "litellm_provider": "openai"
  },
  "no-prices-model": {
    "max_tokens": 4096,
    "litellm_provider": "unknown"
  }
}`

func newTestScraper(srv *httptest.Server) *Scraper {
	return &Scraper{client: srv.Client(), url: srv.URL}
}

type wantModel struct {
	provider           string
	inputCostPerToken  float64
	outputCostPerToken float64
	contextWindow      *int
}

func intPtr(v int) *int { return &v }

func TestFetch_ParsesModelsCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validLiteLLMResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("want 3 models, got %d", len(models))
	}

	// Use a slug-keyed map because LiteLLM returns a JSON object (non-deterministic order).
	want := map[string]wantModel{
		"gpt-4": {
			provider:           "openai",
			inputCostPerToken:  0.00003,
			outputCostPerToken: 0.00006,
			contextWindow:      intPtr(8192),
		},
		"anthropic/claude-3-opus-20240229": {
			provider:           "anthropic",
			inputCostPerToken:  0.000015,
			outputCostPerToken: 0.000075,
			contextWindow:      intPtr(200000),
		},
		"gemini/gemini-pro": {
			provider:           "gemini",
			inputCostPerToken:  0.0000005,
			outputCostPerToken: 0.0000015,
			contextWindow:      intPtr(32000),
		},
	}

	for _, m := range models {
		exp, ok := want[m.Slug]
		if !ok {
			t.Errorf("unexpected model slug: %s", m.Slug)
			continue
		}
		if m.SourceName != "litellm" {
			t.Errorf("%s: want source=litellm, got %s", m.Slug, m.SourceName)
		}
		if m.FetchedAt.IsZero() {
			t.Errorf("%s: FetchedAt should not be zero", m.Slug)
		}
		if m.Provider != exp.provider {
			t.Errorf("%s: want provider=%s, got %s", m.Slug, exp.provider, m.Provider)
		}
		if m.InputCostPerToken != exp.inputCostPerToken {
			t.Errorf("%s: want input=%f, got %f", m.Slug, exp.inputCostPerToken, m.InputCostPerToken)
		}
		if m.OutputCostPerToken != exp.outputCostPerToken {
			t.Errorf("%s: want output=%f, got %f", m.Slug, exp.outputCostPerToken, m.OutputCostPerToken)
		}
		if exp.contextWindow != nil {
			if m.ContextWindow == nil || *m.ContextWindow != *exp.contextWindow {
				t.Errorf("%s: want context_window=%d, got %v", m.Slug, *exp.contextWindow, m.ContextWindow)
			}
		}
	}
}

func TestFetch_ProviderFromLiteLLMProviderField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validLiteLLMResponse))
	}))
	defer srv.Close()

	models, _ := newTestScraper(srv).Fetch(context.Background())

	// Find gpt-4 — provider should come from litellm_provider field
	for _, m := range models {
		if m.Slug == "gpt-4" {
			if m.Provider != "openai" {
				t.Errorf("want provider=openai from litellm_provider field, got %s", m.Provider)
			}
			return
		}
	}
	t.Fatal("gpt-4 not found in results")
}

func TestFetch_ProviderFromSlugWhenNoLiteLLMProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validLiteLLMResponse))
	}))
	defer srv.Close()

	models, _ := newTestScraper(srv).Fetch(context.Background())

	// gemini/gemini-pro has no litellm_provider — provider extracted from slug prefix
	for _, m := range models {
		if m.Slug == "gemini/gemini-pro" {
			if m.Provider != "gemini" {
				t.Errorf("want provider=gemini from slug, got %s", m.Provider)
			}
			return
		}
	}
	t.Fatal("gemini/gemini-pro not found in results")
}

func TestFetch_SkipsModelsWithMissingPrices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(missingPricesLiteLLMResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 model (gpt-4 only), got %d", len(models))
	}
}

func TestFetch_SkipsModelsWithZeroPrices(t *testing.T) {
	const resp = `{
		"free-model": {
			"input_cost_per_token": 0.0,
			"output_cost_per_token": 0.0,
			"litellm_provider": "meta"
		},
		"paid-model": {
			"input_cost_per_token": 0.00003,
			"output_cost_per_token": 0.00006,
			"litellm_provider": "openai"
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 model (paid-model only), got %d", len(models))
	}
	if models[0].Slug != "paid-model" {
		t.Errorf("want paid-model, got %s", models[0].Slug)
	}
}

func TestFetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

func TestFetch_ReturnsErrorOnInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json {"))
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for invalid JSON")
	}
}

func TestFetch_ContextWindowFromMaxInputTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validLiteLLMResponse))
	}))
	defer srv.Close()

	models, _ := newTestScraper(srv).Fetch(context.Background())

	for _, m := range models {
		if m.Slug == "gpt-4" {
			if m.ContextWindow == nil || *m.ContextWindow != 8192 {
				t.Errorf("want context_window=8192 from max_input_tokens, got %v", m.ContextWindow)
			}
			return
		}
	}
	t.Fatal("gpt-4 not found")
}

func TestFetch_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validLiteLLMResponse))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := newTestScraper(srv).Fetch(ctx)
	if err == nil {
		t.Fatal("want error for cancelled context")
	}
}

func TestNew_NilClientUsesDefault(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("New(nil) returned nil")
	}
	if s.url != defaultURL {
		t.Errorf("want url=%q, got %q", defaultURL, s.url)
	}
}

func TestNew_CustomClient(t *testing.T) {
	custom := &http.Client{}
	s := New(custom)
	if s == nil {
		t.Fatal("New(custom) returned nil")
	}
}

func TestModalityFromMode_Audio(t *testing.T) {
	if got := modalityFromMode("audio_transcription"); got != "audio" {
		t.Errorf("audio_transcription: want %q, got %q", "audio", got)
	}
	if got := modalityFromMode("audio_speech"); got != "audio" {
		t.Errorf("audio_speech: want %q, got %q", "audio", got)
	}
}

func TestModalityFromMode_Multimodal(t *testing.T) {
	if got := modalityFromMode("multimodal"); got != "multimodal" {
		t.Errorf("multimodal: want %q, got %q", "multimodal", got)
	}
}

func TestModalityFromMode_UnknownDefault(t *testing.T) {
	if got := modalityFromMode("reranking"); got != "text" {
		t.Errorf("reranking: want %q (default), got %q", "text", got)
	}
}

func TestFetch_ModalityFromModeField(t *testing.T) {
	const resp = `{
		"text-embedding-ada-002": {
			"input_cost_per_token": 0.0000001,
			"output_cost_per_token": 0.0000001,
			"litellm_provider": "openai",
			"mode": "embedding"
		},
		"dall-e-3": {
			"input_cost_per_token": 0.00004,
			"output_cost_per_token": 0.00004,
			"litellm_provider": "openai",
			"mode": "image_generation"
		},
		"gpt-4": {
			"input_cost_per_token": 0.00003,
			"output_cost_per_token": 0.00006,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"text-embedding-ada-002": "embedding",
		"dall-e-3":               "image",
		"gpt-4":                  "text",
	}
	for _, m := range models {
		if expected, ok := want[m.Slug]; ok {
			if m.Modality != expected {
				t.Errorf("slug=%s: want modality=%s, got %s", m.Slug, expected, m.Modality)
			}
		}
	}
}
