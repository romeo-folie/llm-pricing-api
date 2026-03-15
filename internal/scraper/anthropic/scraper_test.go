package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// minimalPricingHTML is a structurally faithful replica of the Anthropic
// pricing page trimmed to the cases under test.
const minimalPricingHTML = `<!DOCTYPE html><html><body>

<h2>Model pricing</h2>
<table class="w-full border-collapse">
  <thead>
    <tr>
      <th>Model</th>
      <th>Base Input Tokens</th>
      <th>5m Cache Writes</th>
      <th>1h Cache Writes</th>
      <th>Cache Hits &amp; Refreshes</th>
      <th>Output Tokens</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>Claude Opus 4.6</td>
      <td>$5 / MTok</td>
      <td>$6.25 / MTok</td>
      <td>$10 / MTok</td>
      <td>$0.50 / MTok</td>
      <td>$25 / MTok</td>
    </tr>
    <tr>
      <td>Claude Sonnet 4.5</td>
      <td>$3 / MTok</td>
      <td>$3.75 / MTok</td>
      <td>$6 / MTok</td>
      <td>$0.30 / MTok</td>
      <td>$15 / MTok</td>
    </tr>
    <tr>
      <td>Claude Haiku 3.5 (deprecated)</td>
      <td>$0.80 / MTok</td>
      <td>$1 / MTok</td>
      <td>$2 / MTok</td>
      <td>$0.08 / MTok</td>
      <td>$4 / MTok</td>
    </tr>
    <tr>
      <td>Claude Opus 4</td>
      <td>$15 / MTok</td>
      <td>$18.75 / MTok</td>
      <td>$30 / MTok</td>
      <td>$1.50 / MTok</td>
      <td>$75 / MTok</td>
    </tr>
    <tr>
      <td>No Price Model</td>
      <td>-</td>
      <td>-</td>
      <td>-</td>
      <td>-</td>
      <td>-</td>
    </tr>
  </tbody>
</table>

<!-- Batch processing section — must be skipped -->
<h2>Batch processing</h2>
<table class="w-full border-collapse">
  <thead>
    <tr>
      <th>Model</th>
      <th>Batch input</th>
      <th>Batch output</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>Claude Opus 4.6</td>
      <td>$2.50 / MTok</td>
      <td>$12.50 / MTok</td>
    </tr>
  </tbody>
</table>

<!-- Tool use pricing — must be skipped (no model-name + input/output columns) -->
<h2>Tool use pricing</h2>
<table class="w-full border-collapse">
  <thead>
    <tr>
      <th>Tool</th>
      <th>Additional input tokens</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>text_editor_20250429</td>
      <td>700 tokens</td>
    </tr>
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

	// Expect Claude Opus 4.6, Claude Sonnet 4.5, Claude Haiku 3.5
	// (deprecated suffix stripped but model included; no-price row skipped).
	// Batch and tool-use sections skipped.
	// Canonical slug format: claude-<major>-<minor>-<variant>
	// e.g. "Claude Opus 4.6" → "claude-4-6-opus"
	want := map[string]struct{}{
		"anthropic/claude-4-6-opus":   {},
		"anthropic/claude-4-5-sonnet": {},
		"anthropic/claude-3-5-haiku":  {},
		"anthropic/claude-4-opus":     {}, // major-only: "Claude Opus 4"
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
		if m.Provider != "anthropic" {
			t.Errorf("%s: want Provider=anthropic, got %s", m.Slug, m.Provider)
		}
		if m.SourceName != "anthropic" {
			t.Errorf("%s: want SourceName=anthropic, got %s", m.Slug, m.SourceName)
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
	}

	for slug := range want {
		if _, ok := got[slug]; !ok {
			t.Errorf("missing expected model: %s", slug)
		}
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

		t.Fatalf("Fetch returned unexpected error: %v", err)
	}

	found := false
	for _, m := range models {
		// Canonical slug for "Claude Opus 4.6" is "claude-4-6-opus"
		if m.Slug != "anthropic/claude-4-6-opus" {
			continue
		}
		found = true
		// $5 / MTok → 5 / 1,000,000
		const wantInput = 5.0 / 1_000_000
		if m.InputCostPerToken != wantInput {
			t.Errorf("claude-4-6-opus InputCostPerToken: got %v, want %v", m.InputCostPerToken, wantInput)
		}
		// $25 / MTok → 25 / 1,000,000
		const wantOutput = 25.0 / 1_000_000
		if m.OutputCostPerToken != wantOutput {
			t.Errorf("claude-4-6-opus OutputCostPerToken: got %v, want %v", m.OutputCostPerToken, wantOutput)
		}
	}
	if !found {
		t.Error("expected model anthropic/claude-4-6-opus not found in scraped results")
	}
}

func TestFetch_DeprecatedSuffix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {

		t.Fatalf("Fetch returned unexpected error: %v", err)
	}

	// "Claude Haiku 3.5 (deprecated)" should appear as "claude-3-5-haiku"
	// (canonical format: version-first, variant-last; parenthetical stripped).
	found := false
	for _, m := range models {
		if m.Slug == "anthropic/claude-3-5-haiku" {
			found = true
		}
		// "(deprecated)" slug must not be present.
		if strings.Contains(m.Slug, "deprecated") {
			t.Errorf("slug must not contain 'deprecated': %s", m.Slug)
		}
	}
	if !found {
		t.Errorf("expected slug anthropic/claude-3-5-haiku for deprecated model")
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

func TestParsePricePerMTok(t *testing.T) {
	const eps = 1e-16
	abs := func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}

	cases := []struct {
		input   string
		wantVal float64
		wantErr bool
	}{
		{"$5 / MTok", 5.0 / 1_000_000, false},
		{"$25 / MTok", 25.0 / 1_000_000, false},
		{"$0.50 / MTok", 0.50 / 1_000_000, false},
		{"$0.08 / MTok", 0.08 / 1_000_000, false},
		{"-", 0, true},
		{"—", 0, true},
		{"", 0, true},
		{"not-a-number", 0, true},
		{"$0 / MTok", 0, true}, // zero price rejected
	}

	for _, tc := range cases {
		v, err := parsePricePerMTok(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parsePricePerMTok(%q): wantErr=%v, got err=%v", tc.input, tc.wantErr, err)
			continue
		}
		if !tc.wantErr && abs(v-tc.wantVal) > eps {
			t.Errorf("parsePricePerMTok(%q): got %v, want %v", tc.input, v, tc.wantVal)
		}
	}
}

func TestCleanModelName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Claude Opus 4.6", "Claude Opus 4.6"},
		{"Claude Haiku 3.5 (deprecated)", "Claude Haiku 3.5"},
		{"Claude Sonnet 3.7 (deprecated)", "Claude Sonnet 3.7"},
		{"Claude Next (preview)", "Claude Next"},
		{"  Claude Opus 4  ", "Claude Opus 4"},
	}
	for _, tc := range cases {
		if got := cleanModelName(tc.in); got != tc.want {
			t.Errorf("cleanModelName(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalAnthropicSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		// Major.Minor format → claude-<major>-<minor>-<variant>
		{"Claude Opus 4.6", "claude-4-6-opus"},
		{"Claude Sonnet 4.5", "claude-4-5-sonnet"},
		{"Claude Haiku 3.5", "claude-3-5-haiku"},
		{"Claude Sonnet 3.7", "claude-3-7-sonnet"},
		// Major-only format → claude-<major>-<variant>
		{"Claude Opus 4", "claude-4-opus"},
		{"Claude Opus 3", "claude-3-opus"},
		{"Claude Sonnet 5", "claude-5-sonnet"},
		// Parenthetical suffix stripped before slugging
		{"Claude Haiku 3.5 (deprecated)", "claude-3-5-haiku"},
		{"Claude Next (beta)", "claude-next"},
		// Fallback: unknown format → lowercase-hyphenated
		{"Some Future Model", "some-future-model"},
		{"claude-already-a-slug", "claude-already-a-slug"},
	}
	for _, tc := range cases {
		if got := canonicalAnthropicSlug(tc.in); got != tc.want {
			t.Errorf("canonicalAnthropicSlug(%q): got %q, want %q", tc.in, got, tc.want)
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


