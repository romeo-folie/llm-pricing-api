package openai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Fixtures -------------------------------------------------------------
//
// The live page is an Astro site: each pricing component is an <astro-island>
// whose `props` attribute holds serialized JSON, and whose children are the
// server-rendered table markup. These fixtures reproduce that shape — including
// the &quot; character-reference encoding the real page uses — without the
// hundreds of kilobytes of collapsed table markup.

// island renders an <astro-island> with component-export set and the given
// serialized props JSON, character-reference encoded the way the live page
// encodes it.
func island(component, propsJSON string) string {
	escaped := strings.NewReplacer(`"`, "&quot;").Replace(propsJSON)
	return `<astro-island component-export="` + component + `" props="` + escaped + `">` +
		`<div>server-rendered collapsed table</div></astro-island>`
}

// propsStandard mirrors the standard-tier TextTokenPricingTables island. Rows
// vary in arity because the cache-write column is omitted for some models; the
// "-" and "Free" rows must be skipped.
const propsStandard = `{"tier":[0,"standard"],"collapsedLatestRowCount":[0,4],"rows":[1,[` +
	`[1,[[0,"gpt-6-astra"],[0,10],[0,1],[0,12.5],[0,50]]],` +
	`[1,[[0,"gpt-4o"],[0,2.5],[0,1.25],[0,10]]],` +
	`[1,[[0,"gpt-4.1"],[0,2],[0,0.5],[0,8]]],` +
	`[1,[[0,"gpt-5.5 (<272K context length)"],[0,5],[0,0.5],[0,"-"],[0,30]]],` +
	`[1,[[0,"no-price-model"],[0,"-"],[0,"-"],[0,"-"]]],` +
	`[1,[[0,"free-model"],[0,"Free"],[0,"-"],[0,"-"]]],` +
	`[1,[[0,"truncated-row"],[0,5]]]` +
	`]]}`

// propsBatch is the same component on a non-standard tier; it must be ignored.
const propsBatch = `{"tier":[0,"batch"],"rows":[1,[` +
	`[1,[[0,"gpt-6-astra"],[0,5],[0,0.5],[0,6.25],[0,25]]]` +
	`]]}`

// propsDaybreak uses the model-per-group layout with short/long context columns.
const propsDaybreak = `{"headings":[1,[[0,"Model"],[0,"Short context input"],[0,"Short context cached input"],[0,"Short context cache writes"],[0,"Short context output"],[0,"Long context input"],[0,"Long context cached input"],[0,"Long context cache writes"],[0,"Long context output"]]],"groups":[1,[` +
	`[0,{"model":[0,"gpt-5.6-sol"],"rows":[1,[[1,[[0,4],[0,0.4],[0,5],[0,20],[0,8],[0,0.8],[0,10],[0,30]]]]]}],` +
	`[0,{"model":[0,"gpt-5.6-cyber"],"rows":[1,[[1,[[0,12.5],[0,1.25],[0,15.625],[0,75],[0,"-"],[0,"-"],[0,"-"],[0,"-"]]]]]}]` +
	`]]}`

// propsSpecialized uses the category-per-group layout; the model name is the
// first value of each row and the embedding row has no output price.
const propsSpecialized = `{"headings":[1,[[0,"Category"],[0,"Model"],[0,"Input"],[0,"Cached input"],[0,"Output"]]],"groups":[1,[` +
	`[0,{"model":[0,"ChatGPT"],"rows":[1,[[1,[[0,"chat-latest"],[0,5],[0,0.5],[0,30]]]]]}],` +
	`[0,{"model":[0,"Embedding"],"rows":[1,[[1,[[0,"text-embedding-3-small"],[0,0.02],[0,"-"],[0,"-"]]]]]}]` +
	`]]}`

// propsImage is a grouped table whose section is not in the per-token allowlist.
const propsImage = `{"headings":[1,[[0,"Model"],[0,"Modality"],[0,"Input"],[0,"Cached input"],[0,"Output"]]],"groups":[1,[` +
	`[0,{"model":[0,"gpt-image-2.5"],"rows":[1,[[1,[[0,"Image"],[0,8],[0,2],[0,30]]]]]}]` +
	`]]}`

// propsFinetuning uses a component this scraper does not handle at all.
const propsFinetuning = `{"headings":[1,[[0,"Model"],[0,"Training"],[0,"Input"],[0,"Cached input"],[0,"Output"]]],"rows":[1,[` +
	`[1,[[0,"o4-mini-2025-04-16"],[0,"$100.00 / hour"],[0,4],[0,1],[0,16]]]` +
	`]]}`

// pricingPage assembles the fixture sections in document order.
func pricingPage() string {
	return `<!DOCTYPE html><html><body>` +
		`<h2 id="text-tokens"><p>Flagship models</p></h2>` +
		`<div class="pricing-switcher-subheading">Our latest models</div>` +
		island(componentTextTokenPricing, propsStandard) +
		island(componentTextTokenPricing, propsBatch) +
		`<div class="pricing-switcher-subheading">Our latest Daybreak models.</div>` +
		island(componentGroupedPricing, propsDaybreak) +
		`<h2>Image generation models</h2>` +
		island(componentGroupedPricing, propsImage) +
		`<h2>Specialized models</h2>` +
		island(componentGroupedPricing, propsSpecialized) +
		`<h2>Finetuning</h2>` +
		island("PricingTable", propsFinetuning) +
		`</body></html>`
}

func serve(t *testing.T, body string) *Scraper {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Scraper{client: srv.Client(), url: srv.URL}
}

// --- Fetch ----------------------------------------------------------------

func TestFetch(t *testing.T) {
	s := serve(t, pricingPage())

	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	want := map[string]struct{}{
		"openai/gpt-6-astra":   {},
		"openai/gpt-4o":        {},
		"openai/gpt-4.1":       {},
		"openai/gpt-5.5":       {}, // parenthetical context annotation stripped
		"openai/gpt-5.6-sol":   {},
		"openai/gpt-5.6-cyber": {},
		"openai/chat-latest":   {},
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

// TestFetch_ExcludedSources pins everything the scraper must *not* publish:
// non-standard tiers, unit-priced sections, unhandled components and rows
// without a usable price.
func TestFetch_ExcludedSources(t *testing.T) {
	s := serve(t, pricingPage())

	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	excluded := []string{
		"openai/no-price-model",                 // "-" input
		"openai/free-model",                     // "Free" input
		"openai/gpt-image-2.5",                  // image section
		"openai/text-embedding-3-small",         // no output price
		"openai/o4-mini-2025-04-16",             // unhandled component
		"openai/truncated-row",                  // name + input only; output would be misread
		"openai/gpt-5.5-(<272k-context-length)", // annotation must not leak into the slug
	}
	for _, m := range models {
		for _, bad := range excluded {
			if m.Slug == bad {
				t.Errorf("model %s must not be published", bad)
			}
		}
	}

	// The batch tier prices gpt-6-astra at 5/25; standard is 10/50 and must win.
	for _, m := range models {
		if m.Slug != "openai/gpt-6-astra" {
			continue
		}
		if got, want := m.InputCostPerToken, 10.0/1_000_000; got != want {
			t.Errorf("gpt-6-astra input: got %v, want standard-tier %v", got, want)
		}
		if got, want := m.OutputCostPerToken, 50.0/1_000_000; got != want {
			t.Errorf("gpt-6-astra output: got %v, want standard-tier %v", got, want)
		}
	}
}

func TestFetch_PriceConversion(t *testing.T) {
	s := serve(t, pricingPage())

	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cases := map[string]struct{ input, output float64 }{
		"openai/gpt-4o":      {2.5 / 1_000_000, 10.0 / 1_000_000},
		"openai/gpt-6-astra": {10.0 / 1_000_000, 50.0 / 1_000_000},
		"openai/gpt-5.6-sol": {4.0 / 1_000_000, 20.0 / 1_000_000}, // short context
		"openai/chat-latest": {5.0 / 1_000_000, 30.0 / 1_000_000}, // category layout
		"openai/gpt-4.1":     {2.0 / 1_000_000, 8.0 / 1_000_000},  // 3-price row
		"openai/gpt-5.5":     {5.0 / 1_000_000, 30.0 / 1_000_000}, // "-" cache write column
	}

	got := make(map[string]scraperPrices, len(models))
	for _, m := range models {
		got[m.Slug] = scraperPrices{m.InputCostPerToken, m.OutputCostPerToken}
	}

	for slug, want := range cases {
		p, ok := got[slug]
		if !ok {
			t.Errorf("missing expected model: %s", slug)
			continue
		}
		if p.input != want.input || p.output != want.output {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", slug, p.input, p.output, want.input, want.output)
		}
	}
}

type scraperPrices struct{ input, output float64 }

func TestFetch_Deduplication(t *testing.T) {
	// The same model can appear in more than one text table (standard and
	// Daybreak). The first occurrence wins.
	standard := `{"tier":[0,"standard"],"rows":[1,[[1,[[0,"gpt-4o"],[0,2.5],[0,10]]]]]}`
	daybreak := `{"headings":[1,[[0,"Model"],[0,"Short context input"],[0,"Short context output"]]],"groups":[1,[` +
		`[0,{"model":[0,"gpt-4o"],"rows":[1,[[1,[[0,9.99],[0,99.9]]]]]}]` +
		`]]}`

	page := `<!DOCTYPE html><html><body>` +
		island(componentTextTokenPricing, standard) +
		`<div class="pricing-switcher-subheading">Our latest Daybreak models.</div>` +
		island(componentGroupedPricing, daybreak) +
		`</body></html>`

	s := serve(t, page)
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 deduplicated model, got %d", len(models))
	}
	if want := 2.5 / 1_000_000; models[0].InputCostPerToken != want {
		t.Errorf("first occurrence must win: got input %v, want %v", models[0].InputCostPerToken, want)
	}
}

// TestFetch_OnlyFirstPanelPerSection pins the tier-panel rule: a section with a
// service-tier switcher emits one grouped island per panel, standard first, and
// a fast-mode-only model must never be published as standard pricing.
func TestFetch_OnlyFirstPanelPerSection(t *testing.T) {
	standard := `{"headings":[1,[[0,"Category"],[0,"Model"],[0,"Input"],[0,"Output"]]],"groups":[1,[` +
		`[0,{"model":[0,"Codex"],"rows":[1,[[1,[[0,"gpt-5.3-codex"],[0,1.75],[0,14]]]]]}]` +
		`]]}`
	fast := `{"headings":[1,[[0,"Category"],[0,"Model"],[0,"Input"],[0,"Output"]]],"groups":[1,[` +
		`[0,{"model":[0,"Codex"],"rows":[1,[[1,[[0,"gpt-5.3-codex"],[0,3.5],[0,28]]]]]}],` +
		`[0,{"model":[0,"Codex"],"rows":[1,[[1,[[0,"gpt-5.3-fast-only"],[0,9],[0,90]]]]]}]` +
		`]]}`

	page := `<!DOCTYPE html><html><body><h2>Specialized models</h2>` +
		island(componentGroupedPricing, standard) +
		island(componentGroupedPricing, fast) +
		`</body></html>`

	s := serve(t, page)
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		for _, m := range models {
			t.Logf("  got: %s", m.Slug)
		}
		t.Fatalf("got %d models, want 1", len(models))
	}
	if models[0].Slug != "openai/gpt-5.3-codex" {
		t.Errorf("got slug %s, want openai/gpt-5.3-codex", models[0].Slug)
	}
	if want := 1.75 / 1_000_000; models[0].InputCostPerToken != want {
		t.Errorf("standard panel must win: got input %v, want %v", models[0].InputCostPerToken, want)
	}
}

// TestFetch_BatchOnlyPageFails verifies that a page carrying only non-standard
// tiers surfaces as an error rather than a silent empty success — the failure
// mode that let #209 go unnoticed.
func TestFetch_BatchOnlyPageFails(t *testing.T) {
	page := `<!DOCTYPE html><html><body>` +
		island(componentTextTokenPricing, propsBatch) +
		`</body></html>`

	s := serve(t, page)
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when only non-standard tiers are present, got nil")
	}
}

// TestFetch_TextTokenIslandWithoutTierFails verifies that an island whose tier
// prop disappeared is skipped loudly rather than silently treated as standard.
func TestFetch_TextTokenIslandWithoutTierFails(t *testing.T) {
	noTier := `{"rows":[1,[[1,[[0,"gpt-4o"],[0,2.5],[0,10]]]]]}`

	s := serve(t, `<!DOCTYPE html><html><body>`+island(componentTextTokenPricing, noTier)+`</body></html>`)
	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when the tier prop is missing, got nil")
	}
}

// TestFetch_SectionWithoutTrailingPeriod verifies section matching is
// punctuation-tolerant: the live heading ends in ".", but it must still match
// if upstream drops it.
func TestFetch_SectionWithoutTrailingPeriod(t *testing.T) {
	daybreak := `{"headings":[1,[[0,"Model"],[0,"Short context input"],[0,"Short context output"]]],"groups":[1,[` +
		`[0,{"model":[0,"gpt-5.6-sol"],"rows":[1,[[1,[[0,4],[0,20]]]]]}]` +
		`]]}`

	page := `<!DOCTYPE html><html><body>` +
		`<div class="pricing-switcher-subheading">Our latest Daybreak models</div>` +
		island(componentGroupedPricing, daybreak) +
		`</body></html>`

	s := serve(t, page)
	models, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 || models[0].Slug != "openai/gpt-5.6-sol" {
		t.Fatalf("got %d models (%v), want [openai/gpt-5.6-sol]", len(models), models)
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
	s := serve(t, `<!DOCTYPE html><html><body></body></html>`)

	_, err := s.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when no models found, got nil")
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

// --- Serialization --------------------------------------------------------

func TestDecodeAstroValue(t *testing.T) {
	// Equivalent plain object: {"tier":"standard","rows":[["a",1],["b",2]],"meta":{"ok":true,"none":null}}
	const raw = `{"tier":[0,"standard"],` +
		`"rows":[1,[[1,[[0,"a"],[0,1]]],[1,[[0,"b"],[0,2]]]]],` +
		`"meta":[0,{"ok":[0,true],"none":[0,null]}]}`

	var encoded any
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	decoded, ok := decodeAstroValue(encoded).(map[string]any)
	if !ok {
		t.Fatalf("decoded props is %T, want map", decodeAstroValue(encoded))
	}

	if tier, _ := decoded["tier"].(string); tier != "standard" {
		t.Errorf("tier: got %v, want standard", decoded["tier"])
	}

	rows, ok := decoded["rows"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("rows: got %#v, want 2 entries", decoded["rows"])
	}
	second, _ := rows[1].([]any)
	if len(second) != 2 || second[0] != "b" || second[1] != float64(2) {
		t.Errorf("rows[1]: got %#v, want [b 2]", rows[1])
	}

	meta, _ := decoded["meta"].(map[string]any)
	if meta["ok"] != true {
		t.Errorf("meta.ok: got %#v, want true", meta["ok"])
	}
	if v, present := meta["none"]; !present || v != nil {
		t.Errorf("meta.none: got %#v (present=%v), want nil", v, present)
	}
}

func TestDecodeIslandProps_CharacterReferences(t *testing.T) {
	// The fallback path: props that are still character-reference encoded.
	props, err := decodeIslandProps(`{&quot;tier&quot;:[0,&quot;standard&quot;]}`)
	if err != nil {
		t.Fatalf("decodeIslandProps: %v", err)
	}
	if tier, _ := propString(props, "tier"); tier != "standard" {
		t.Errorf("tier: got %q, want standard", tier)
	}

	if _, err := decodeIslandProps(""); err == nil {
		t.Error("expected error for missing props attribute")
	}
	if _, err := decodeIslandProps("not json"); err == nil {
		t.Error("expected error for non-JSON props")
	}
}

// --- Row helpers ----------------------------------------------------------

func TestPricePerMillion(t *testing.T) {
	cases := []struct {
		name   string
		input  any
		want   float64
		wantOK bool
	}{
		{"number", 1.25, 1.25, true},
		{"integer", float64(10), 10, true},
		{"dash placeholder", "-", 0, false},
		{"word", "Free", 0, false},
		{"null", nil, 0, false},
		{"zero", float64(0), 0, false},
		{"negative", -1.0, 0, false},
		{"NaN", math.NaN(), 0, false},
		{"Inf", math.Inf(1), 0, false},
	}
	for _, tc := range cases {
		got, ok := pricePerMillion(tc.input)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("%s: pricePerMillion(%v) = (%v, %v), want (%v, %v)",
				tc.name, tc.input, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestPriceColumnIndex(t *testing.T) {
	cols := []string{
		"Short context input",
		"Short context cached input",
		"Short context cache writes",
		"Short context output",
		"Long context input",
		"Long context output",
	}
	if got := priceColumnIndex(cols, "input"); got != 0 {
		t.Errorf("input index: got %d, want 0 (short context, not cached)", got)
	}
	if got := priceColumnIndex(cols, "output"); got != 3 {
		t.Errorf("output index: got %d, want 3 (short context)", got)
	}
	if got := priceColumnIndex([]string{"Cached input", "Output"}, "input"); got != -1 {
		t.Errorf("cached input must not match: got %d, want -1", got)
	}
	// Long-context columns are a different price and must never be selected.
	longFirst := []string{"Long context input", "Long context cached input", "Long context output"}
	if got := priceColumnIndex(longFirst, "input"); got != -1 {
		t.Errorf("long-context input must not match: got %d, want -1", got)
	}
	if got := priceColumnIndex(longFirst, "output"); got != -1 {
		t.Errorf("long-context output must not match: got %d, want -1", got)
	}
	mixed := []string{"Long context input", "Long context output", "Input", "Output"}
	if got := priceColumnIndex(mixed, "input"); got != 2 {
		t.Errorf("input index: got %d, want 2 (skip long context)", got)
	}
	if got := priceColumnIndex(mixed, "output"); got != 3 {
		t.Errorf("output index: got %d, want 3 (skip long context)", got)
	}
}

func TestNormalizeSection(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Specialized models", "specialized models"},
		{"Our latest Daybreak models.", "our latest daybreak models"},
		{"  Our latest Daybreak models  ", "our latest daybreak models"},
	}
	for _, tc := range cases {
		if got := normalizeSection(tc.in); got != tc.want {
			t.Errorf("normalizeSection(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"gpt-4.1", "gpt-4.1"},
		{"gpt-4o", "gpt-4o"},
		{"gpt-5.5 (<272K context length)", "gpt-5.5"},
		{"text-embedding-3-small", "text-embedding-3-small"},
		{"GPT Image 1.5", "gpt-image-1.5"},
	}
	for _, tc := range cases {
		if got := normalizeSlug(tc.in); got != tc.want {
			t.Errorf("normalizeSlug(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanHeading(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Specialized models", "Specialized models"},
		{"Specialized models\ue09a", "Specialized models"},
		{"  Our latest models  ", "Our latest models"},
	}
	for _, tc := range cases {
		if got := cleanHeading(tc.in); got != tc.want {
			t.Errorf("cleanHeading(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}
