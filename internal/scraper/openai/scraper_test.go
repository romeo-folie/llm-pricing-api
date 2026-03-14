package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// minimalPricingHTML is a trimmed but structurally faithful replica of the
// OpenAI pricing page.  It contains:
//   - A "Text tokens" section with a standard two-column (Input/Output) table
//   - A "Text tokens" section with a short/long context table (colSpan headers)
//   - A "Fine-tuning" section that must be skipped (training cost + inference)
//   - An "Image tokens" section that must be skipped (non-text modality)
const minimalPricingHTML = `<!DOCTYPE html><html><body>
<!-- Text tokens section – simple table -->
<span class="heading">Text tokens</span>
<table>
  <thead>
    <tr>
      <th></th>
      <th>Input</th>
      <th>Cached input</th>
      <th>Output</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>gpt-4.1</td><td>$2.00</td><td>$0.50</td><td>$8.00</td></tr>
    <tr><td>gpt-4.1-mini</td><td>$0.40</td><td>$0.10</td><td>$1.60</td></tr>
    <tr><td>gpt-4.1-nano</td><td>$0.10</td><td>$0.025</td><td>$0.40</td></tr>
    <tr><td>no-price-model</td><td>-</td><td>-</td><td>-</td></tr>
  </tbody>
</table>

<!-- Text tokens section – short/long context (colSpan headers) -->
<span class="heading">Text tokens</span>
<table>
  <thead>
    <tr>
      <th></th>
      <th colspan="3">Short context</th>
      <th colspan="3">Long context</th>
    </tr>
    <tr>
      <th></th>
      <th>Input</th>
      <th>Cached input</th>
      <th>Output</th>
      <th>Input</th>
      <th>Cached input</th>
      <th>Output</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>gpt-5.4</td><td>$1.25</td><td>$0.13</td><td>$7.50</td><td>-</td><td>-</td><td>-</td></tr>
    <tr><td>gpt-5.4-pro</td><td>$15.00</td><td>-</td><td>$90.00</td><td>-</td><td>-</td><td>-</td></tr>
  </tbody>
</table>

<!-- Fine-tuning section – must be skipped -->
<span class="heading">Fine-tuning</span>
<table>
  <thead>
    <tr>
      <th></th>
      <th>Training</th>
      <th>Input</th>
      <th>Output</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>gpt-4.1-2025-04-14</td><td>$25.00</td><td>$1.50</td><td>$6.00</td></tr>
  </tbody>
</table>

<!-- Image tokens section – must be skipped -->
<span class="heading">Image tokens</span>
<table>
  <thead>
    <tr>
      <th></th>
      <th>Input</th>
      <th>Cached Input</th>
      <th>Output</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>gpt-image-1</td><td>$8.00</td><td>$2.00</td><td>$32.00</td></tr>
  </tbody>
</table>
</body></html>`

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	// Expect: gpt-4.1, gpt-4.1-mini, gpt-4.1-nano, gpt-5.4, gpt-5.4-pro
	// (no-price-model skipped; fine-tuning and image sections skipped)
	want := map[string]struct{}{
		"openai/gpt-4-1":      {},
		"openai/gpt-4-1-mini": {},
		"openai/gpt-4-1-nano": {},
		"openai/gpt-5-4":      {},
		"openai/gpt-5-4-pro":  {},
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
			t.Errorf("unexpected model slug: %s", m.Slug)
		}
		if m.Provider != "openai" {
			t.Errorf("%s: want Provider=openai, got %s", m.Slug, m.Provider)
		}
		if m.SourceName != "openai" {
			t.Errorf("%s: want SourceName=openai, got %s", m.Slug, m.SourceName)
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
			t.Errorf("missing expected model: %s", slug)
		}
	}
}

func TestFetch_PriceConversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(minimalPricingHTML))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, _ := s.Fetch(context.Background())

	for _, m := range models {
		if m.Slug != "openai/gpt-4-1" {
			continue
		}
		// $2.00 / 1M = 0.000002
		const wantInput = 2.0 / 1_000_000
		if m.InputCostPerToken != wantInput {
			t.Errorf("gpt-4.1 InputCostPerToken: got %v, want %v", m.InputCostPerToken, wantInput)
		}
		// $8.00 / 1M = 0.000008
		const wantOutput = 8.0 / 1_000_000
		if m.OutputCostPerToken != wantOutput {
			t.Errorf("gpt-4.1 OutputCostPerToken: got %v, want %v", m.OutputCostPerToken, wantOutput)
		}
	}
}

func TestFetch_Deduplication(t *testing.T) {
	// The page can have duplicate tables (e.g. flex + standard pricing).
	// The first occurrence of each model wins; subsequent duplicates are dropped.
	html := `<!DOCTYPE html><html><body>
<span class="heading">Text tokens</span>
<table>
  <thead><tr><th></th><th>Input</th><th>Output</th></tr></thead>
  <tbody>
    <tr><td>gpt-4o</td><td>$2.50</td><td>$10.00</td></tr>
  </tbody>
</table>
<span class="heading">Text tokens</span>
<table>
  <thead><tr><th></th><th>Input</th><th>Output</th></tr></thead>
  <tbody>
    <tr><td>gpt-4o</td><td>$5.00</td><td>$20.00</td></tr>
  </tbody>
</table>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	s := &Scraper{client: srv.Client(), url: srv.URL}
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 deduplicated model, got %d", len(models))
	}
	// First occurrence ($2.50 input) wins.
	const wantInput = 2.50 / 1_000_000
	if models[0].InputCostPerToken != wantInput {
		t.Errorf("want first-occurrence price %v, got %v", wantInput, models[0].InputCostPerToken)
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
	const eps = 1e-16 // tolerance for IEEE-754 rounding in division
	cases := []struct {
		input   string
		wantVal float64
		wantErr bool
	}{
		{"$1.25", 1.25 / 1_000_000, false},
		{"$100.00", 100.0 / 1_000_000, false},
		{"$0.025", 0.025 / 1_000_000, false},
		{"-", 0, true},
		{"—", 0, true},
		{"", 0, true},
		{"$0.00", 0, true}, // zero price rejected
		{"not-a-number", 0, true},
		{"$100.00 / hour", 100.0 / 1_000_000, false}, // strip trailing text
	}

	abs := func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}

	for _, tc := range cases {
		v, err := parsePricePerMillion(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parsePricePerMillion(%q): wantErr=%v, got err=%v", tc.input, tc.wantErr, err)
			continue
		}
		if !tc.wantErr && abs(v-tc.wantVal) > eps {
			t.Errorf("parsePricePerMillion(%q): got %v, want %v", tc.input, v, tc.wantVal)
		}
	}
}

func TestNormalizeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gpt-4.1", "gpt-4-1"},
		{"GPT Image 1.5", "gpt-image-1-5"},
		{"gpt-4o", "gpt-4o"},
		{"text-embedding-3-small", "text-embedding-3-small"},
	}
	for _, tc := range cases {
		if got := normalizeSlug(tc.in); got != tc.want {
			t.Errorf("normalizeSlug(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestExtractHeaders_ColSpanInLastRow verifies that colSpan attributes on cells
// in the *last* header row are expanded correctly.  This exercises the path
// inside extractHeaders that expandscells with colspan > 1 — a path that the
// top-level Fetch tests don't reach because their colSpan attributes are on the
// *first* header row (the group-label row), not the last one.
func TestExtractHeaders_ColSpanInLastRow(t *testing.T) {
	const raw = `<table>
  <thead>
    <tr>
      <th></th>
      <th colspan="2">Context window</th>
      <th colspan="2">Pricing</th>
    </tr>
    <tr>
      <th></th>
      <th colspan="2">Context window</th>
      <th>Input</th>
      <th>Output</th>
    </tr>
  </thead>
</table>`
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("html.Parse: %v", err)
	}
	// Find the <thead> element.
	var thead *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if thead != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "thead" {
			thead = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if thead == nil {
		t.Fatal("could not locate <thead>")
	}

	got := extractHeaders(thead)
	// Last header row: colspan=2 "Context window" expands to 2 × "Context window",
	// then "Input", "Output".  The first <th> (model name) is skipped.
	want := []string{"Context window", "Context window", "Input", "Output"}
	if len(got) != len(want) {
		t.Fatalf("extractHeaders: got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extractHeaders[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFetch_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client cancels.
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
