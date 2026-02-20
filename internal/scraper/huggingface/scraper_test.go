package huggingface

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"llm-pricing-api/internal/scraper"
)

// happyPathResponse contains two models:
// - meta-llama: together-ai (live, priced) and replicate (loading — should be skipped)
// - mistralai: fireworks-ai (live, priced)
// Expected result after filtering: 2 models (together + fireworks).
const happyPathResponse = `[
  {
    "id": "meta-llama/Meta-Llama-3-70B-Instruct",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "together-ai": {
        "provider": "together-ai",
        "status": "live",
        "pricing": { "input": 0.00000088, "output": 0.00000088 }
      },
      "replicate": {
        "provider": "replicate",
        "status": "loading",
        "pricing": { "input": 0.00000100, "output": 0.00000100 }
      }
    }
  },
  {
    "id": "mistralai/Mistral-7B-Instruct-v0.3",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "fireworks-ai": {
        "provider": "fireworks-ai",
        "status": "live",
        "pricing": { "input": 0.00000020, "output": 0.00000020 }
      }
    }
  }
]`

// allFilteredResponse has only non-live / zero-price providers.
const allFilteredResponse = `[
  {
    "id": "some-org/some-model",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "replicate": {
        "provider": "replicate",
        "status": "loading",
        "pricing": { "input": 0.001, "output": 0.001 }
      },
      "fal-ai": {
        "provider": "fal-ai",
        "status": "live",
        "pricing": { "input": 0.0, "output": 0.0 }
      }
    }
  }
]`

// zeroPriceResponse has one model with live status but zero prices.
const zeroPriceResponse = `[
  {
    "id": "org/model-a",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "hf-inference": {
        "provider": "hf-inference",
        "status": "live",
        "pricing": { "input": 0.0, "output": 0.001 }
      }
    }
  },
  {
    "id": "org/model-b",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "nebius": {
        "provider": "nebius",
        "status": "live",
        "pricing": { "input": 0.001, "output": 0.0 }
      }
    }
  },
  {
    "id": "org/model-c",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "sambanova": {
        "provider": "sambanova",
        "status": "live",
        "pricing": { "input": 0.001, "output": 0.002 }
      }
    }
  }
]`

// wrongPipelineResponse mixes text-generation with another tag.
const wrongPipelineResponse = `[
  {
    "id": "org/text-model",
    "pipeline_tag": "text-generation",
    "inferenceProviderMapping": {
      "together-ai": {
        "provider": "together-ai",
        "status": "live",
        "pricing": { "input": 0.001, "output": 0.002 }
      }
    }
  },
  {
    "id": "org/image-model",
    "pipeline_tag": "text-to-image",
    "inferenceProviderMapping": {
      "fal-ai": {
        "provider": "fal-ai",
        "status": "live",
        "pricing": { "input": 0.001, "output": 0.002 }
      }
    }
  }
]`

func newTestScraper(srv *httptest.Server) *Scraper {
	return &Scraper{client: srv.Client(), baseURL: srv.URL}
}

// TestFetch_HappyPath verifies correct parsing of live, priced models and
// that non-live providers are excluded.
func TestFetch_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(happyPathResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// replicate is loading → skipped; together-ai + fireworks-ai survive.
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}

	// Find the meta-llama entry (served by together-ai).
	var llamaModel *struct {
		slug               string
		provider           string
		underlyingProvider string
		sourceName         string
		modality           string
		input              float64
		output             float64
	}
	for _, m := range models {
		if m.Slug == "together/meta-llama-3-70b-instruct" {
			llamaModel = &struct {
				slug               string
				provider           string
				underlyingProvider string
				sourceName         string
				modality           string
				input              float64
				output             float64
			}{
				slug:               m.Slug,
				provider:           m.Provider,
				underlyingProvider: m.UnderlyingProvider,
				sourceName:         m.SourceName,
				modality:           m.Modality,
				input:              m.InputCostPerToken,
				output:             m.OutputCostPerToken,
			}
			break
		}
	}

	if llamaModel == nil {
		t.Fatal("expected to find model with slug 'together/meta-llama-3-70b-instruct'")
	}

	if llamaModel.provider != "together" {
		t.Errorf("want provider=together, got %q", llamaModel.provider)
	}
	if llamaModel.underlyingProvider != "together" {
		// normProv maps "together-ai" → "together" to match OpenRouter's convention.
		t.Errorf("want UnderlyingProvider=together, got %q", llamaModel.underlyingProvider)
	}
	if llamaModel.sourceName != "huggingface_inference_providers" {
		t.Errorf("want SourceName=huggingface_inference_providers, got %q", llamaModel.sourceName)
	}
	if llamaModel.modality != "text" {
		t.Errorf("want Modality=text, got %q", llamaModel.modality)
	}
	if llamaModel.input != 0.00000088 {
		t.Errorf("want InputCostPerToken=0.00000088, got %g", llamaModel.input)
	}
	if llamaModel.output != 0.00000088 {
		t.Errorf("want OutputCostPerToken=0.00000088, got %g", llamaModel.output)
	}

	// FetchedAt must be non-zero for all models.
	for _, m := range models {
		if m.FetchedAt.IsZero() {
			t.Errorf("model %q has zero FetchedAt", m.Slug)
		}
	}
}

// TestFetch_NonLiveSkipped ensures providers with status != "live" are excluded.
func TestFetch_NonLiveSkipped(t *testing.T) {
	// happyPathResponse has replicate with status=loading — it must not appear.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(happyPathResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, m := range models {
		if m.UnderlyingProvider == "replicate" {
			t.Errorf("replicate provider (loading status) should have been skipped, but got model %q", m.Slug)
		}
	}
}

// TestFetch_ZeroPriceSkipped verifies models with input=0 or output=0 are skipped.
func TestFetch_ZeroPriceSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(zeroPriceResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only sambanova (both prices > 0) should survive.
	if len(models) != 1 {
		t.Fatalf("want 1 model (only sambanova), got %d", len(models))
	}
	if models[0].UnderlyingProvider != "sambanova" {
		t.Errorf("want UnderlyingProvider=sambanova, got %q", models[0].UnderlyingProvider)
	}
}

// TestFetch_Non200Response verifies that a non-200 HTTP status returns an error.
func TestFetch_Non200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response, got nil")
	}
}

// TestFetch_JSONDecodeFailure verifies that invalid JSON returns an error.
func TestFetch_JSONDecodeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json {{"))
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

// TestFetch_EmptyAfterFiltering verifies that if all providers are filtered out,
// Fetch returns an error rather than an empty slice.
func TestFetch_EmptyAfterFiltering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(allFilteredResponse))
	}))
	defer srv.Close()

	_, err := newTestScraper(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("want error when all models are filtered out, got nil")
	}
}

// TestFetch_PipelineTagFilter ensures models with pipeline_tag != "text-generation" are skipped.
func TestFetch_PipelineTagFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(wrongPipelineResponse))
	}))
	defer srv.Close()

	models, err := newTestScraper(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the text-generation model should survive.
	if len(models) != 1 {
		t.Fatalf("want 1 model (text-generation only), got %d", len(models))
	}
	if models[0].Slug != "together/text-model" {
		t.Errorf("want slug=together/text-model, got %q", models[0].Slug)
	}
}

// TestNew_NilClientUsesDefault verifies New(nil) returns a non-nil Scraper
// with the default base URL and a non-nil HTTP client.
func TestNew_NilClientUsesDefault(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("New(nil) returned nil")
	}
	if s.client == nil {
		t.Error("expected non-nil client")
	}
	if s.baseURL != defaultBaseURL {
		t.Errorf("want baseURL=%q, got %q", defaultBaseURL, s.baseURL)
	}
}

// TestNew_CustomClient verifies New with a provided client stores it correctly.
func TestNew_CustomClient(t *testing.T) {
	custom := &http.Client{}
	s := New(custom)
	if s == nil {
		t.Fatal("New(custom) returned nil")
	}
	if s.client != custom {
		t.Error("expected scraper to use provided client")
	}
}

// TestNormalizedProvider_KnownMapping verifies known HF provider IDs are mapped correctly.
func TestNormalizedProvider_KnownMapping(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"together-ai", "together"},
		{"fireworks-ai", "fireworks"},
		{"replicate", "replicate"},
		{"fal-ai", "fal-ai"},
		{"nebius", "nebius"},
		{"hyperbolic", "hyperbolic"},
		{"sambanova", "sambanova"},
		{"hf-inference", "hf-inference"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizedProvider(tc.input)
			if got != tc.want {
				t.Errorf("normalizedProvider(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormalizedProvider_UnknownPassthrough verifies unknown provider IDs pass through unchanged.
func TestNormalizedProvider_UnknownPassthrough(t *testing.T) {
	unknown := "some-unknown-provider"
	got := normalizedProvider(unknown)
	if got != unknown {
		t.Errorf("normalizedProvider(%q) = %q, want passthrough %q", unknown, got, unknown)
	}
}

// TestCheckRedirectHost_BlocksLoopback verifies that IPv4 loopback addresses are blocked.
func TestCheckRedirectHost_BlocksLoopback(t *testing.T) {
	cases := []string{"127.0.0.1", "127.0.0.2"}
	for _, host := range cases {
		t.Run(host, func(t *testing.T) {
			if err := scraper.CheckRedirectHost(context.Background(), host); err == nil {
				t.Errorf("scraper.CheckRedirectHost(%q) = nil, want error (loopback blocked)", host)
			}
		})
	}
}

// TestCheckRedirectHost_BlocksPrivate verifies that RFC-1918 private addresses are blocked.
func TestCheckRedirectHost_BlocksPrivate(t *testing.T) {
	cases := []string{"192.168.1.1", "10.0.0.1", "172.16.0.1"}
	for _, host := range cases {
		t.Run(host, func(t *testing.T) {
			if err := scraper.CheckRedirectHost(context.Background(), host); err == nil {
				t.Errorf("scraper.CheckRedirectHost(%q) = nil, want error (private IP blocked)", host)
			}
		})
	}
}

// TestCheckRedirectHost_BlocksLoopbackWithPort verifies that port stripping works before
// the loopback check (a host of "127.0.0.1:8080" must still be rejected).
func TestCheckRedirectHost_BlocksLoopbackWithPort(t *testing.T) {
	if err := scraper.CheckRedirectHost(context.Background(), "127.0.0.1:8080"); err == nil {
		t.Error(`scraper.CheckRedirectHost("127.0.0.1:8080") = nil, want error`)
	}
}

// TestCheckRedirectHost_AllowsPublicIP verifies that a well-known public IP is not blocked.
// IP literals are resolved without a real DNS query, so this test is deterministic.
func TestCheckRedirectHost_AllowsPublicIP(t *testing.T) {
	if err := scraper.CheckRedirectHost(context.Background(), "8.8.8.8"); err != nil {
		t.Errorf(`scraper.CheckRedirectHost("8.8.8.8") = %v, want nil`, err)
	}
}

// TestCheckRedirectHost_UnresolvableHostBlocked verifies that a host that cannot be
// resolved is treated as blocked rather than allowed (fail-closed).
func TestCheckRedirectHost_UnresolvableHostBlocked(t *testing.T) {
	if err := scraper.CheckRedirectHost(context.Background(), "this-host-definitely-does-not-exist.invalid"); err == nil {
		t.Error("scraper.CheckRedirectHost with unresolvable host should return error (fail-closed)")
	}
}

// TestNew_NilClient_SSRFPrevention verifies that the default client (New(nil)) blocks
// all TCP connections to loopback/private addresses via the Transport's DialContext hook.
// This covers the SSRF prevention requirement from CLAUDE.md (custom http.Transport).
func TestNew_NilClient_SSRFPrevention(t *testing.T) {
	s := New(nil)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1/test", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	_, err = s.client.Do(req)
	if err == nil {
		t.Error("expected connection to loopback to be blocked (SSRF prevention), got nil error")
	}
}

// TestNormalizeModelName covers various org-prefix and casing scenarios.
func TestNormalizeModelName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"meta-llama/Meta-Llama-3-70B-Instruct", "meta-llama-3-70b-instruct"},
		{"mistralai/Mistral-7B-Instruct-v0.3", "mistral-7b-instruct-v0-3"},
		{"org/Model_With_Underscores", "model-with-underscores"},
		{"NoSlash", "noslash"},
		{"org/UPPER-CASE", "upper-case"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeModelName(tc.input)
			if got != tc.want {
				t.Errorf("normalizeModelName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFetch_ContextCancellation verifies that a cancelled context propagates correctly.
func TestFetch_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(happyPathResponse))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := newTestScraper(srv).Fetch(ctx)
	if err == nil {
		t.Fatal("want error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled in error chain, got: %v", err)
	}
}
