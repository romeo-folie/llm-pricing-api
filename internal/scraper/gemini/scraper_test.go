package gemini

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const floatEps = 1e-14

func floatEq(a, b float64) bool { return math.Abs(a-b) <= floatEps }

// minimalPricingHTML is a structurally faithful replica of the Gemini pricing
// page trimmed to the key cases under test.
const minimalPricingHTML = `<!DOCTYPE html>
<html lang="en"><body>

<h1>Gemini Developer API pricing</h1>

<!-- Simple standard model with text-only prices -->
<h2>Gemini 2.5 Pro</h2>
<h3>Standard</h3>
<table class="pricing-table">
  <thead>
    <tr>
      <th></th>
      <th scope="col">Free Tier</th>
      <th scope="col">Paid Tier, per 1M tokens in USD</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>Input price</td><td>Free of charge</td><td>$0.25 (text / image / video)</td></tr>
    <tr><td>Output price (including thinking tokens)</td><td>Free of charge</td><td>$3.00</td></tr>
    <tr><td>Context caching price</td><td>Free of charge</td><td>$0.03125</td></tr>
    <tr><td>Grounding with Google Search</td><td>Not available</td><td>$14 / 1,000 queries</td></tr>
    <tr><td>Used to improve our products</td><td>Yes</td><td>No</td></tr>
  </tbody>
</table>

<h3>Batch</h3>
<table class="pricing-table">
  <thead>
    <tr>
      <th></th>
      <th scope="col">Free Tier</th>
      <th scope="col">Paid Tier, per 1M tokens in USD</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>Input price</td><td>Not available</td><td>$0.125 (text / image / video)</td></tr>
    <tr><td>Output price (including thinking tokens)</td><td>Not available</td><td>$1.50</td></tr>
  </tbody>
</table>

<!-- Model with tiered context-window prices -->
<h2>Gemini 3.1 Pro Preview</h2>
<h3>Standard</h3>
<table class="pricing-table">
  <thead>
    <tr>
      <th></th>
      <th scope="col">Free Tier</th>
      <th scope="col">Paid Tier, per 1M tokens in USD</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>Input price</td><td>Not available</td><td>$2.00, prompts &lt;= 200k tokens
$4.00, prompts &gt; 200k tokens</td></tr>
    <tr><td>Output price (including thinking tokens)</td><td>Not available</td><td>$12.00, prompts &lt;= 200k tokens
$18.00, prompts &gt; 200k</td></tr>
  </tbody>
</table>

<!-- Model with emoji in name -->
<h2>Gemini 3.1 Flash Image Preview 🍌</h2>
<h3>Standard</h3>
<table class="pricing-table">
  <thead>
    <tr>
      <th></th>
      <th scope="col">Free Tier</th>
      <th scope="col">Paid Tier, per 1M tokens in USD</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>Input price</td><td>Not available</td><td>$0.50 (text/image)</td></tr>
    <tr><td>Output price (including thinking tokens)</td><td>Not available</td><td>$3 (text and thinking)
$60.00 (images)</td></tr>
  </tbody>
</table>

<!-- Model with "Not available" paid tier — must be skipped -->
<h2>Gemini 3 Pro Preview</h2>
<h3>Standard</h3>
<table class="pricing-table">
  <thead>
    <tr>
      <th></th>
      <th scope="col">Free Tier</th>
      <th scope="col">Paid Tier, per 1M tokens in USD</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>Input price</td><td>Not available</td><td>Not available</td></tr>
    <tr><td>Output price (including thinking tokens)</td><td>Not available</td><td>Not available</td></tr>
  </tbody>
</table>

</body></html>`

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	// Expect: gemini-2.5-pro, gemini-3.1-pro-preview, gemini-3.1-flash-image-preview
	// Batch tiers skipped; "Gemini 3 Pro Preview" (no paid price) skipped.
	// Dots are preserved in slugs (consistent with OpenAI scraper convention).
	want := map[string]struct{}{
		"google/gemini-2.5-pro":                {},
		"google/gemini-3.1-pro-preview":        {},
		"google/gemini-3.1-flash-image-preview": {},
	}

	if len(models) != len(want) {
		t.Errorf("got %d models, want %d", len(models), len(want))
		for _, m := range models {
			t.Logf("  got: %s", m.Slug)
		}
	}

	got := make(map[string]struct{}, len(models))
	for _, m := range models {
		got[m.Slug] = struct{}{}
		if _, ok := want[m.Slug]; !ok {
			t.Errorf("unexpected slug: %s", m.Slug)
		}
		if m.Provider != "google" {
			t.Errorf("%s: want Provider=google, got %s", m.Slug, m.Provider)
		}
		if m.SourceName != "google" {
			t.Errorf("%s: want SourceName=google, got %s", m.Slug, m.SourceName)
		}
		if m.Modality != "text" {
			t.Errorf("%s: want Modality=text, got %s", m.Slug, m.Modality)
		}
		if m.InputCostPerToken <= 0 {
			t.Errorf("%s: InputCostPerToken must be positive, got %v", m.Slug, m.InputCostPerToken)
		}
		if m.OutputCostPerToken <= 0 {
			t.Errorf("%s: OutputCostPerToken must be positive, got %v", m.Slug, m.OutputCostPerToken)
		}
		if m.FetchedAt.IsZero() {
			t.Errorf("%s: FetchedAt must not be zero", m.Slug)
		}
	}

	for slug := range want {
		if _, ok := got[slug]; !ok {
			t.Errorf("missing expected slug: %s", slug)
		}
	}
}

func TestFetch_BatchTierSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	// Batch tier for Gemini 2.5 Pro should not produce a second entry.
	count := 0
	for _, m := range models {
		if m.Slug == "google/gemini-2.5-pro" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("gemini-2.5-pro appeared %d times, want exactly 1 (Standard only)", count)
	}
}

func TestFetch_PriceConversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	found := false
	for _, m := range models {
		if m.Slug != "google/gemini-2.5-pro" {
			continue
		}
		found = true
		// $0.25 / 1M = 0.00000025
		const wantInput = 0.25 / 1_000_000
		if !floatEq(m.InputCostPerToken, wantInput) {
			t.Errorf("gemini-2.5-pro InputCostPerToken: got %v, want %v", m.InputCostPerToken, wantInput)
		}
		// $3.00 / 1M = 0.000003
		const wantOutput = 3.00 / 1_000_000
		if !floatEq(m.OutputCostPerToken, wantOutput) {
			t.Errorf("gemini-2.5-pro OutputCostPerToken: got %v, want %v", m.OutputCostPerToken, wantOutput)
		}
	}
	if !found {
		t.Error("expected model google/gemini-2.5-pro not found in scraped results")
	}
}

func TestFetch_TieredContextPrice(t *testing.T) {
	// For models with ≤200k / >200k pricing in the same cell, the first
	// (lower-context) price is used.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	found := false
	for _, m := range models {
		if m.Slug != "google/gemini-3.1-pro-preview" {
			continue
		}
		found = true
		const wantInput = 2.00 / 1_000_000  // first line: $2.00
		const wantOutput = 12.00 / 1_000_000 // first line: $12.00
		if !floatEq(m.InputCostPerToken, wantInput) {
			t.Errorf("tiered input: got %v, want %v", m.InputCostPerToken, wantInput)
		}
		if !floatEq(m.OutputCostPerToken, wantOutput) {
			t.Errorf("tiered output: got %v, want %v", m.OutputCostPerToken, wantOutput)
		}
	}
	if !found {
		t.Error("expected model google/gemini-3.1-pro-preview not found in scraped results")
	}
}

func TestFetch_EmojiStripped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	foundImagePreview := false
	for _, m := range models {
		if strings.Contains(m.Slug, "image-preview") {
			foundImagePreview = true
			// Must NOT contain the 🍌 emoji or any non-ASCII in the slug.
			for _, r := range m.Slug {
				if r > 127 {
					t.Errorf("slug contains non-ASCII character %q: %s", r, m.Slug)
				}
			}
		}
	}
	if !foundImagePreview {
		t.Error("expected at least one image-preview slug in scraped results (emoji-stripping not exercised)")
	}
}

func TestFetch_MultiLineOutputTextPrice(t *testing.T) {
	// Output cell: "$3 (text and thinking)\n$60.00 (images)"
	// Only the text price ($3) should be used.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	found := false
	for _, m := range models {
		if m.Slug != "google/gemini-3.1-flash-image-preview" {
			continue
		}
		found = true
		const wantOutput = 3.0 / 1_000_000
		if !floatEq(m.OutputCostPerToken, wantOutput) {
			t.Errorf("multi-line output: got %v, want %v", m.OutputCostPerToken, wantOutput)
		}
	}
	if !found {
		t.Error("expected model google/gemini-3.1-flash-image-preview not found in scraped results")
	}
}

func TestFetch_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error on non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status 503, got: %v", err)
	}
}

func TestFetch_EmptyHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body></body></html>`))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when no models found, got nil")
	}
}

func TestParsePricePerMillion(t *testing.T) {
	cases := []struct {
		input   string
		wantVal float64
		wantErr bool
	}{
		{"$0.25 (text / image / video)", 0.25 / 1_000_000, false},
		{"$3.00", 3.00 / 1_000_000, false},
		{"$3 (text and thinking)\n$60.00 (images)", 3.0 / 1_000_000, false},
		{"$2.00, prompts <= 200k tokens\n$4.00, prompts > 200k tokens", 2.0 / 1_000_000, false},
		{"$1,000.00", 1000.0 / 1_000_000, false},
		{"Not available", 0, true},
		{"Free of charge", 0, true},
		{"", 0, true},
		{"no dollar sign here", 0, true},
	}

	for _, tc := range cases {
		v, err := parsePricePerMillion(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parsePricePerMillion(%q): wantErr=%v, got err=%v", tc.input, tc.wantErr, err)
			continue
		}
		if !tc.wantErr && !floatEq(v, tc.wantVal) {
			t.Errorf("parsePricePerMillion(%q): got %v, want %v", tc.input, v, tc.wantVal)
		}
	}
}

func TestCleanModelName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Gemini 2.5 Pro", "Gemini 2.5 Pro"},
		{"Gemini 3.1 Flash Image Preview 🍌", "Gemini 3.1 Flash Image Preview"},
		{"  Gemini 2.5 Flash  ", "Gemini 2.5 Flash"},
	}
	for _, tc := range cases {
		if got := cleanModelName(tc.in); got != tc.want {
			t.Errorf("cleanModelName(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Gemini 2.5 Pro", "gemini-2.5-pro"},
		{"Gemini 3.1 Flash Image Preview", "gemini-3.1-flash-image-preview"},
		{"Gemini 2.5 Flash-Lite", "gemini-2.5-flash-lite"},
		{"Gemini 2.5 Flash_Lite", "gemini-2.5-flash-lite"}, // underscores → hyphens
	}
	for _, tc := range cases {
		if got := normalizeSlug(tc.in); got != tc.want {
			t.Errorf("normalizeSlug(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFetch_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	_, err := s.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
}
