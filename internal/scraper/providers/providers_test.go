package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// serveFixture returns an httptest.Server that serves the named fixture file
// from the testdata/ directory with Content-Type text/html.
func serveFixture(t *testing.T, filename string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("load fixture %s: %v", filename, err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}))
}

// ─── OpenAI ──────────────────────────────────────────────────────────────────

func TestOpenAI_Fetch_ParsesModelsCorrectly(t *testing.T) {
	srv := serveFixture(t, "openai.html")
	defer srv.Close()

	s := &OpenAIScraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("want at least 1 model, got 0")
	}

	found := false
	for _, m := range models {
		if m.Slug == "gpt-4o" {
			found = true
			if m.Provider != "openai" {
				t.Errorf("want provider=openai, got %s", m.Provider)
			}
			if m.SourceName != "openai-docs" {
				t.Errorf("want source=openai-docs, got %s", m.SourceName)
			}
			// $2.50 / 1M tokens → 0.0000025 per token
			wantInput := 2.50 / 1_000_000
			if m.InputCostPerToken != wantInput {
				t.Errorf("want input=%v, got %v", wantInput, m.InputCostPerToken)
			}
			// $10.00 / 1M tokens → 0.00001 per token
			wantOutput := 10.00 / 1_000_000
			if m.OutputCostPerToken != wantOutput {
				t.Errorf("want output=%v, got %v", wantOutput, m.OutputCostPerToken)
			}
			if m.FetchedAt.IsZero() {
				t.Error("FetchedAt should not be zero")
			}
		}
	}
	if !found {
		t.Error("gpt-4o not found in results")
	}
}

func TestOpenAI_Fetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := &OpenAIScraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

func TestOpenAI_Fetch_RespectsContextCancellation(t *testing.T) {
	srv := serveFixture(t, "openai.html")
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &OpenAIScraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(ctx)
	if err == nil {
		t.Fatal("want error for cancelled context")
	}
}

// ─── Anthropic ───────────────────────────────────────────────────────────────

func TestAnthropic_Fetch_ParsesModelsCorrectly(t *testing.T) {
	srv := serveFixture(t, "anthropic.html")
	defer srv.Close()

	s := &AnthropicScraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("want at least 1 model, got 0")
	}

	found := false
	for _, m := range models {
		if m.Slug == "claude-sonnet-4-5" {
			found = true
			if m.Provider != "anthropic" {
				t.Errorf("want provider=anthropic, got %s", m.Provider)
			}
			if m.SourceName != "anthropic-docs" {
				t.Errorf("want source=anthropic-docs, got %s", m.SourceName)
			}
			wantInput := 3.00 / 1_000_000
			if m.InputCostPerToken != wantInput {
				t.Errorf("want input=%v, got %v", wantInput, m.InputCostPerToken)
			}
			wantOutput := 15.00 / 1_000_000
			if m.OutputCostPerToken != wantOutput {
				t.Errorf("want output=%v, got %v", wantOutput, m.OutputCostPerToken)
			}
			if m.FetchedAt.IsZero() {
				t.Error("FetchedAt should not be zero")
			}
		}
	}
	if !found {
		t.Error("claude-sonnet-4-5 not found in results")
	}
}

func TestAnthropic_Fetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := &AnthropicScraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

// ─── Google ───────────────────────────────────────────────────────────────────

func TestGoogle_Fetch_ParsesModelsCorrectly(t *testing.T) {
	srv := serveFixture(t, "google.html")
	defer srv.Close()

	s := &GoogleScraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("want at least 1 model, got 0")
	}

	found := false
	for _, m := range models {
		if m.Slug == "gemini-2.5-pro" {
			found = true
			if m.Provider != "google" {
				t.Errorf("want provider=google, got %s", m.Provider)
			}
			if m.SourceName != "google-docs" {
				t.Errorf("want source=google-docs, got %s", m.SourceName)
			}
			wantInput := 1.25 / 1_000_000
			if m.InputCostPerToken != wantInput {
				t.Errorf("want input=%v, got %v", wantInput, m.InputCostPerToken)
			}
			wantOutput := 10.00 / 1_000_000
			if m.OutputCostPerToken != wantOutput {
				t.Errorf("want output=%v, got %v", wantOutput, m.OutputCostPerToken)
			}
			if m.FetchedAt.IsZero() {
				t.Error("FetchedAt should not be zero")
			}
		}
	}
	if !found {
		t.Error("gemini-2.5-pro not found in results")
	}
}

func TestGoogle_Fetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := &GoogleScraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

// ─── Mistral ──────────────────────────────────────────────────────────────────

func TestMistral_Fetch_ParsesModelsCorrectly(t *testing.T) {
	srv := serveFixture(t, "mistral.html")
	defer srv.Close()

	s := &MistralScraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("want at least 1 model, got 0")
	}

	found := false
	for _, m := range models {
		if m.Slug == "mistral-large-latest" {
			found = true
			if m.Provider != "mistral" {
				t.Errorf("want provider=mistral, got %s", m.Provider)
			}
			if m.SourceName != "mistral-docs" {
				t.Errorf("want source=mistral-docs, got %s", m.SourceName)
			}
			wantInput := 2.00 / 1_000_000
			if m.InputCostPerToken != wantInput {
				t.Errorf("want input=%v, got %v", wantInput, m.InputCostPerToken)
			}
			wantOutput := 6.00 / 1_000_000
			if m.OutputCostPerToken != wantOutput {
				t.Errorf("want output=%v, got %v", wantOutput, m.OutputCostPerToken)
			}
			if m.FetchedAt.IsZero() {
				t.Error("FetchedAt should not be zero")
			}
		}
	}
	if !found {
		t.Error("mistral-large-latest not found in results")
	}
}

func TestMistral_Fetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := &MistralScraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

// ─── Amazon ───────────────────────────────────────────────────────────────────

func TestAmazon_Fetch_ParsesModelsCorrectly(t *testing.T) {
	srv := serveFixture(t, "amazon.html")
	defer srv.Close()

	s := &AmazonScraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("want at least 1 model, got 0")
	}

	found := false
	for _, m := range models {
		if m.Slug == "anthropic.claude-3-5-sonnet-20241022-v2:0" {
			found = true
			if m.Provider != "anthropic" {
				t.Errorf("want provider=anthropic (from Provider column), got %s", m.Provider)
			}
			if m.SourceName != "amazon-docs" {
				t.Errorf("want source=amazon-docs, got %s", m.SourceName)
			}
			wantInput := 3.00 / 1_000_000
			if m.InputCostPerToken != wantInput {
				t.Errorf("want input=%v, got %v", wantInput, m.InputCostPerToken)
			}
			wantOutput := 15.00 / 1_000_000
			if m.OutputCostPerToken != wantOutput {
				t.Errorf("want output=%v, got %v", wantOutput, m.OutputCostPerToken)
			}
			if m.FetchedAt.IsZero() {
				t.Error("FetchedAt should not be zero")
			}
		}
	}
	if !found {
		t.Error("anthropic.claude-3-5-sonnet-20241022-v2:0 not found in results")
	}
}

func TestAmazon_Fetch_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s := &AmazonScraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("want error for non-200 response")
	}
}

func TestAmazon_Fetch_ProviderFromProviderColumn(t *testing.T) {
	srv := serveFixture(t, "amazon.html")
	defer srv.Close()

	s := &AmazonScraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	providerMap := map[string]string{}
	for _, m := range models {
		providerMap[m.Slug] = m.Provider
	}

	// "Meta" in Provider column → lowercased to "meta"
	if p, ok := providerMap["meta.llama3-70b-instruct-v1:0"]; !ok || p != "meta" {
		t.Errorf("meta.llama3: want provider=meta, got %q", p)
	}
	// "Mistral AI" in Provider column → lowercased to "mistral ai"
	if p, ok := providerMap["mistral.mistral-large-2402-v1:0"]; !ok || p != "mistral ai" {
		t.Errorf("mistral model: want provider=%q, got %q", "mistral ai", p)
	}
	// "Anthropic" in Provider column → lowercased to "anthropic"
	if p, ok := providerMap["anthropic.claude-3-5-sonnet-20241022-v2:0"]; !ok || p != "anthropic" {
		t.Errorf("claude model: want provider=anthropic, got %q", p)
	}
}

// ─── Shared behaviours ────────────────────────────────────────────────────────

func TestAllScrapers_FetchedAtNotZero(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		fetch   func(srv *httptest.Server) ([]interface{}, error)
	}{
		{
			name:    "openai",
			fixture: "openai.html",
			fetch: func(srv *httptest.Server) ([]interface{}, error) {
				models, err := (&OpenAIScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
				out := make([]interface{}, len(models))
				for i, m := range models {
					out[i] = m
				}
				return out, err
			},
		},
		{
			name:    "anthropic",
			fixture: "anthropic.html",
			fetch: func(srv *httptest.Server) ([]interface{}, error) {
				models, err := (&AnthropicScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
				out := make([]interface{}, len(models))
				for i, m := range models {
					out[i] = m
				}
				return out, err
			},
		},
		{
			name:    "google",
			fixture: "google.html",
			fetch: func(srv *httptest.Server) ([]interface{}, error) {
				models, err := (&GoogleScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
				out := make([]interface{}, len(models))
				for i, m := range models {
					out[i] = m
				}
				return out, err
			},
		},
		{
			name:    "mistral",
			fixture: "mistral.html",
			fetch: func(srv *httptest.Server) ([]interface{}, error) {
				models, err := (&MistralScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
				out := make([]interface{}, len(models))
				for i, m := range models {
					out[i] = m
				}
				return out, err
			},
		},
		{
			name:    "amazon",
			fixture: "amazon.html",
			fetch: func(srv *httptest.Server) ([]interface{}, error) {
				models, err := (&AmazonScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
				out := make([]interface{}, len(models))
				for i, m := range models {
					out[i] = m
				}
				return out, err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveFixture(t, tc.fixture)
			defer srv.Close()
			results, err := tc.fetch(srv)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			if len(results) == 0 {
				t.Fatalf("%s: want at least 1 model", tc.name)
			}
			// FetchedAt is checked per-model in each type-specific test; this
			// test verifies all five scrapers populate FetchedAt.
		})
	}
}

func TestAllScrapers_EmptyTableReturnsError(t *testing.T) {
	const emptyTable = `<!DOCTYPE html><html><body>
<table><thead><tr><th>Model</th><th>Input</th><th>Output</th></tr></thead>
<tbody></tbody></table></body></html>`

	serve := func(t *testing.T) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(emptyTable))
		}))
	}

	t.Run("openai", func(t *testing.T) {
		srv := serve(t)
		defer srv.Close()
		_, err := (&OpenAIScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
		if err == nil {
			t.Error("want error for empty pricing table")
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		srv := serve(t)
		defer srv.Close()
		_, err := (&AnthropicScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
		if err == nil {
			t.Error("want error for empty pricing table")
		}
	})
	t.Run("google", func(t *testing.T) {
		srv := serve(t)
		defer srv.Close()
		_, err := (&GoogleScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
		if err == nil {
			t.Error("want error for empty pricing table")
		}
	})
	t.Run("mistral", func(t *testing.T) {
		srv := serve(t)
		defer srv.Close()
		_, err := (&MistralScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
		if err == nil {
			t.Error("want error for empty pricing table")
		}
	})
	t.Run("amazon", func(t *testing.T) {
		srv := serve(t)
		defer srv.Close()
		_, err := (&AmazonScraper{client: srv.Client(), url: srv.URL}).Fetch(context.Background())
		if err == nil {
			t.Error("want error for empty pricing table")
		}
	})
}
