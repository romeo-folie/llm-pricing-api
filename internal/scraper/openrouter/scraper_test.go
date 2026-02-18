package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const validResponse = `{
  "data": [
    {
      "id": "openai/gpt-4",
      "pricing": {"prompt": "0.00003", "completion": "0.00006"},
      "context_length": 128000,
      "architecture": {"modality": "text"}
    },
    {
      "id": "anthropic/claude-3-opus",
      "pricing": {"prompt": "0.000015", "completion": "0.000075"},
      "context_length": 200000,
      "architecture": {"modality": "text"}
    }
  ]
}`

const missingPricesResponse = `{
  "data": [
    {
      "id": "openai/gpt-4",
      "pricing": {"prompt": "0.00003", "completion": "0.00006"},
      "context_length": 8192,
      "architecture": {"modality": "text"}
    },
    {
      "id": "some/free-model",
      "pricing": {"prompt": "0", "completion": "0"},
      "context_length": 4096,
      "architecture": {"modality": "text"}
    },
    {
      "id": "bad/no-pricing",
      "pricing": {},
      "context_length": 4096,
      "architecture": {"modality": "text"}
    }
  ]
}`

func newTestScraper(srv *httptest.Server) *Scraper {
	return &Scraper{client: srv.Client(), baseURL: srv.URL}
}

func TestFetch_ParsesModelsCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}

	gpt4 := models[0]
	if gpt4.Slug != "openai/gpt-4" {
		t.Errorf("want slug=openai/gpt-4, got %s", gpt4.Slug)
	}
	if gpt4.Provider != "openai" {
		t.Errorf("want provider=openai, got %s", gpt4.Provider)
	}
	if gpt4.InputCostPerToken != 0.00003 {
		t.Errorf("want input=0.00003, got %f", gpt4.InputCostPerToken)
	}
	if gpt4.OutputCostPerToken != 0.00006 {
		t.Errorf("want output=0.00006, got %f", gpt4.OutputCostPerToken)
	}
	if gpt4.ContextWindow == nil || *gpt4.ContextWindow != 128000 {
		t.Errorf("want context_window=128000, got %v", gpt4.ContextWindow)
	}
	if gpt4.SourceName != "openrouter" {
		t.Errorf("want source=openrouter, got %s", gpt4.SourceName)
	}
	if gpt4.FetchedAt.IsZero() {
		t.Error("FetchedAt should not be zero")
	}
}

func TestFetch_SkipsModelsWithZeroOrMissingPrices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(missingPricesResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only gpt-4 has valid prices; free-model (price=0) and no-pricing are skipped.
	if len(models) != 1 {
		t.Fatalf("want 1 model, got %d", len(models))
	}
	if models[0].Slug != "openai/gpt-4" {
		t.Errorf("want openai/gpt-4, got %s", models[0].Slug)
	}
}

func TestFetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

func TestFetch_ReturnsErrorOnInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for invalid JSON")
	}
}

func TestFetch_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validResponse))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := newTestScraper(srv).Fetch(ctx)
	if err == nil {
		t.Fatal("want error for cancelled context")
	}
}

func TestFetch_ProviderExtractedFromSlug(t *testing.T) {
	const resp = `{"data":[{"id":"mistral/mistral-large","pricing":{"prompt":"0.000008","completion":"0.000024"},"context_length":32000,"architecture":{"modality":"text"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil || len(models) != 1 {
		t.Fatalf("unexpected: err=%v, len=%d", err, len(models))
	}
	if models[0].Provider != "mistral" {
		t.Errorf("want provider=mistral, got %s", models[0].Provider)
	}
}

func TestNew_NilClientUsesDefault(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("New(nil) returned nil")
	}
	if s.baseURL != defaultBaseURL {
		t.Errorf("want baseURL=%q, got %q", defaultBaseURL, s.baseURL)
	}
}

func TestNew_CustomClient(t *testing.T) {
	custom := &http.Client{}
	s := New(custom)
	if s == nil {
		t.Fatal("New(custom) returned nil")
	}
}

func TestProviderFromSlug_NoSlash(t *testing.T) {
	got := providerFromSlug("no-slash-here")
	if got != "" {
		t.Errorf("want empty string for slug with no slash, got %q", got)
	}
}

func TestProviderFromSlug_WithSlash(t *testing.T) {
	got := providerFromSlug("openai/gpt-4")
	if got != "openai" {
		t.Errorf("want %q, got %q", "openai", got)
	}
}
