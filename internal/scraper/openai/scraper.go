// Package openai implements a scraper for the OpenAI API pricing page.
//
// The page's 2026 redesign moved prices out of static HTML tables and onto
// Astro islands: each pricing component is an <astro-island> element carrying
// its data as serialized JSON props, and the server-rendered <table> markup is
// *collapsed* to a handful of "latest" rows. Two consequences broke the
// previous table scraper for roughly 190 days (#209):
//
//  1. Section headings were renamed ("Text tokens" → "Our latest models"), so
//     the section allowlist matched nothing and every table was dropped.
//  2. Even with matching headings, the DOM exposes only the collapsed rows — a
//     fraction of the data the page actually carries.
//
// This scraper reads the embedded component props instead. They carry the full
// model list and an explicit service tier, and are unaffected by row
// collapsing.
//
// Prices in the props are USD per 1 million tokens; this scraper converts them
// to per-token cost before returning ScrapedModels:
//
//	props price ($/1M) ÷ 1,000,000 → InputCostPerToken / OutputCostPerToken
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"

	"llm-pricing-api/internal/scraper"
)

// defaultURL is the canonical OpenAI API pricing page.  This must match the
// URL stored in the sources table (migrations/000009_update_openai_source_url).
const defaultURL = "https://developers.openai.com/api/docs/pricing?latest-pricing=standard"

// standardTier is the service tier treated as ground truth. The page also
// carries batch, flex and fast (priority) panels whose per-model prices differ
// from standard processing; publishing those from the same source would have
// the source disagree with itself across runs.
const standardTier = "standard"

// astro-island component-export values whose props this scraper understands.
const (
	componentTextTokenPricing = "TextTokenPricingTables"
	componentGroupedPricing   = "GroupedPricingTable"
)

// textPricingSections lists the page sections whose GroupedPricingTable props
// carry per-token text prices. Keys are normalised by normalizeSection. The
// section is still checked — not just the component name — because the same
// component renders unit-priced tables (image, audio, video, tools) whose
// values are not per-token rates.
var textPricingSections = map[string]struct{}{
	"our latest daybreak models": {},
	"specialized models":         {},
}

// normalizeSection lower-cases and trims a section label and drops a trailing
// period, so "Our latest Daybreak models." and "Our latest Daybreak models"
// compare equal.
func normalizeSection(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimSuffix(s, ".")
}

// Scraper fetches model pricing from the OpenAI API pricing page.
type Scraper struct {
	client *http.Client
	url    string
}

// New returns a Scraper using the provided HTTP client.
// If client is nil, a default client with a 60 s timeout and SSRF prevention
// is used.
func New(client *http.Client) *Scraper {
	if client == nil {
		client = &http.Client{
			Timeout:   60 * time.Second,
			Transport: scraper.NewSSRFSafeTransport(),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return scraper.CheckRedirectHost(req.Context(), req.URL.Hostname())
			},
		}
	}
	return &Scraper{client: client, url: defaultURL}
}

// --- Intermediate representation -----------------------------------------

// pricingIsland is one decoded <astro-island> pricing component together with
// the nearest section heading that preceded it in the document.
type pricingIsland struct {
	// Component is the island's component-export value.
	Component string
	// Section is the closest preceding heading-like text (e.g. "Specialized
	// models"), used to filter unit-priced tables out.
	Section string
	// Props is the de-serialized component props object.
	Props map[string]any
}

// scrapedRow is one model row extracted from an island, before slug and price
// normalisation. Both prices are USD per million tokens.
type scrapedRow struct {
	Name       string
	InputPerM  float64
	OutputPerM float64
}

// --- Fetch ----------------------------------------------------------------

// Fetch retrieves the OpenAI pricing page, extracts the standard-tier text
// pricing components, and returns one ScrapedModel per model that carries a
// usable input and output price.
//
// Batch, flex and fast tiers are skipped (alternate service pricing), as are
// sections whose prices are per unit rather than per token (image, audio,
// video, transcription, tools, fine-tuning, GPT-Live sessions).
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	// The OpenAI developer portal requires a real browser User-Agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("openai: unexpected status", "status", resp.StatusCode, "body_excerpt", string(buf[:n]))
		return nil, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
	}

	islands, err := parsePricingIslands(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: parse html: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := toScrapedModels(islands, fetchedAt)

	if len(models) == 0 {
		return nil, fmt.Errorf("openai: no models returned after filtering — page structure may have changed")
	}

	slog.Debug("openai: scraped models", "count", len(models), "islands", len(islands))
	return models, nil
}

// --- Island extraction ----------------------------------------------------

// parsePricingIslands walks the HTML document, tracking the nearest preceding
// heading-like text, and decodes the props of every pricing island it meets.
//
// Islands themselves are opaque to the walker: their children are the
// server-rendered (and row-collapsed) table markup, which is exactly what this
// scraper exists to avoid.
func parsePricingIslands(r io.Reader) ([]pricingIsland, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var (
		islands []pricingIsland
		section string
	)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4":
				// Real heading elements are unconditionally treated as section
				// labels; no class check needed.
				if t := cleanHeading(textContent(n)); t != "" {
					section = t
				}
			case "span", "div", "p":
				// Some section labels are rendered as <div class="…heading…">
				// (e.g. "pricing-switcher-subheading"). isHeadingLike filters
				// by class so only labelled elements update the section.
				if isHeadingLike(n) {
					if t := cleanHeading(textContent(n)); t != "" {
						section = t
					}
				}
			case "astro-island":
				component := attrValue(n, "component-export")
				if component != componentTextTokenPricing && component != componentGroupedPricing {
					break // not a pricing island; keep walking its children
				}
				props, err := decodeIslandProps(attrValue(n, "props"))
				if err != nil {
					slog.Warn("openai: undecodable pricing island",
						"component", component, "section", section, "err", err)
					return
				}
				islands = append(islands, pricingIsland{
					Component: component,
					Section:   section,
					Props:     props,
				})
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return islands, nil
}

// decodeIslandProps turns an island's props attribute into a plain Go map.
//
// The HTML parser decodes character references in attribute values, so raw is
// normally plain JSON. One explicit unescape is attempted as a fallback for
// payloads that arrive still escaped.
func decodeIslandProps(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, fmt.Errorf("missing props attribute")
	}
	var encoded any
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		if fallbackErr := json.Unmarshal([]byte(html.UnescapeString(raw)), &encoded); fallbackErr != nil {
			return nil, fmt.Errorf("decode props json: %w (after unescaping: %v)", err, fallbackErr)
		}
	}
	decoded := decodeAstroValue(encoded)
	props, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("props is %T, want object", decoded)
	}
	return props, nil
}

// decodeAstroValue converts Astro's serialized prop format into plain Go
// values. Every node is a two-element tuple [type, value]:
//
//	[0, primitive]  a string, number, boolean or null
//	[0, {…}]        an object whose values are themselves encoded
//	[1, […]]        an array of encoded values
//
// Only these two type tags are emitted by the pricing components.
func decodeAstroValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, item := range value {
			out[k] = decodeAstroValue(item)
		}
		return out
	case []any:
		if len(value) == 2 {
			// Compare the raw float so a non-integral tag falls through to the
			// element-wise fallback instead of being truncated onto 0 or 1.
			if tag, ok := value[0].(float64); ok {
				switch tag {
				case 0:
					// Primitives and the object wrapper. Recurse so a
					// nested object/array under tag 0 is decoded too.
					return decodeAstroValue(value[1])
				case 1:
					items, ok := value[1].([]any)
					if !ok {
						return []any{}
					}
					out := make([]any, 0, len(items))
					for _, item := range items {
						out = append(out, decodeAstroValue(item))
					}
					return out
				}
			}
		}
		// Unknown shape — decode element-wise rather than dropping data.
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, decodeAstroValue(item))
		}
		return out
	default:
		return v
	}
}

// --- Conversion to ScrapedModel -------------------------------------------

// toScrapedModels converts decoded islands into ScrapedModels. The first row
// seen for a slug wins; later duplicates (e.g. a model listed in both the
// standard text table and the Daybreak table) are dropped.
//
// GroupedPricingTable islands carry no tier of their own: a section with a
// service-tier switcher ("Specialized models") emits one island per panel, in
// option order, with the standard panel first. Only the first island in each
// section is read so that a fast-mode-only model can never be published as
// standard pricing.
func toScrapedModels(islands []pricingIsland, fetchedAt time.Time) []scraper.ScrapedModel {
	var models []scraper.ScrapedModel
	seen := make(map[string]bool)
	seenGroupedSections := make(map[string]bool)

	for _, isl := range islands {
		if isl.Component == componentGroupedPricing {
			key := normalizeSection(isl.Section)
			if seenGroupedSections[key] {
				slog.Debug("openai: skipping later panel in section", "section", isl.Section)
				continue
			}
			seenGroupedSections[key] = true
		}

		for _, row := range islandRows(isl) {
			slug := "openai/" + normalizeSlug(row.Name)
			if slug == "openai/" || seen[slug] {
				continue
			}
			seen[slug] = true

			models = append(models, scraper.ScrapedModel{
				Slug:               slug,
				Provider:           "openai",
				InputCostPerToken:  row.InputPerM / 1_000_000,
				OutputCostPerToken: row.OutputPerM / 1_000_000,
				Modality:           "text",
				SourceName:         "openai",
				FetchedAt:          fetchedAt,
			})
		}
	}

	return models
}

// islandRows dispatches on component type and applies the tier/section filters.
func islandRows(isl pricingIsland) []scrapedRow {
	switch isl.Component {
	case componentTextTokenPricing:
		tier, ok := propString(isl.Props, "tier")
		if !ok || strings.TrimSpace(tier) == "" {
			slog.Warn("openai: text-token island has no tier", "section", isl.Section)
			return nil
		}
		if !strings.EqualFold(strings.TrimSpace(tier), standardTier) {
			slog.Debug("openai: skipping non-standard tier", "tier", tier)
			return nil
		}
		return textTokenRows(isl.Props)
	case componentGroupedPricing:
		if _, ok := textPricingSections[normalizeSection(isl.Section)]; !ok {
			slog.Debug("openai: skipping non-text section", "section", isl.Section)
			return nil
		}
		return groupedPricingRows(isl.Props)
	default:
		return nil
	}
}

// textTokenRows extracts rows from a TextTokenPricingTables island.
//
// Each row is [name, input, cached input, (cache writes,) output] in USD per
// million tokens. The intermediate cache columns come and go between models,
// but the input price is always first and the output price always last.
func textTokenRows(props map[string]any) []scrapedRow {
	raw, ok := propSlice(props, "rows")
	if !ok {
		return nil
	}

	rows := make([]scrapedRow, 0, len(raw))
	for _, rowValue := range raw {
		row, ok := rowValue.([]any)
		// A usable row is at least name + input + output. A shorter row would
		// make the "last element is the output price" rule read a cache column.
		if !ok || len(row) < 3 {
			continue
		}
		name, ok := row[0].(string)
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		input, ok := pricePerMillion(row[1])
		if !ok {
			slog.Debug("openai: skipping row — invalid input price", "model", name, "raw", row[1])
			continue
		}
		output, ok := pricePerMillion(row[len(row)-1])
		if !ok {
			slog.Debug("openai: skipping row — invalid output price", "model", name, "raw", row[len(row)-1])
			continue
		}
		rows = append(rows, scrapedRow{Name: name, InputPerM: input, OutputPerM: output})
	}
	return rows
}

// groupedPricingRows extracts rows from a GroupedPricingTable island.
//
// The first heading names the group column ("Category" or "Model") and each
// group's row values align with the remaining headings. Two layouts exist on
// the page, and the first heading distinguishes them:
//
//	Category, Model, Input, Cached input, Output
//	    → group.model is a category; each row names its own model
//	Model, Short context input, …, Short context output, Long context …
//	    → group.model is the model; rows hold prices only
//
// Only the first (short-context) input/output pair is used — tiered
// long-context rates are not representable as a single per-token price.
func groupedPricingRows(props map[string]any) []scrapedRow {
	headings := stringSlice(props["headings"])
	if len(headings) < 2 {
		return nil
	}
	columns := headings[1:]

	modelInRow := strings.EqualFold(strings.TrimSpace(columns[0]), "model")
	if modelInRow {
		columns = columns[1:]
	}

	inputIdx := priceColumnIndex(columns, "input")
	outputIdx := priceColumnIndex(columns, "output")
	if inputIdx < 0 || outputIdx < 0 {
		slog.Debug("openai: skipping grouped table without per-token input/output columns",
			"headings", headings)
		return nil
	}

	groups, ok := propSlice(props, "groups")
	if !ok {
		return nil
	}

	var rows []scrapedRow
	for _, groupValue := range groups {
		group, ok := groupValue.(map[string]any)
		if !ok {
			continue
		}
		groupName, _ := propString(group, "model")
		groupRows, _ := propSlice(group, "rows")

		for _, rowValue := range groupRows {
			values, ok := rowValue.([]any)
			if !ok {
				continue
			}
			name := groupName
			if modelInRow {
				if len(values) == 0 {
					continue
				}
				rowName, ok := values[0].(string)
				if !ok {
					continue
				}
				name = rowName
				values = values[1:]
			}
			name = strings.TrimSpace(name)
			if name == "" || inputIdx >= len(values) || outputIdx >= len(values) {
				continue
			}
			input, ok := pricePerMillion(values[inputIdx])
			if !ok {
				continue
			}
			output, ok := pricePerMillion(values[outputIdx])
			if !ok {
				continue
			}
			rows = append(rows, scrapedRow{Name: name, InputPerM: input, OutputPerM: output})
		}
	}
	return rows
}

// priceColumnIndex returns the index of the per-token price column whose
// heading is want ("input" or "output").
//
// Matching is an explicit allowlist — the bare label or its short-context
// variant — rather than a suffix match. A suffix match would accept "Cached
// input", "Long context input", "Batch input" and "Flex input", all of which
// are different prices.
func priceColumnIndex(headings []string, want string) int {
	for i, h := range headings {
		label := strings.ToLower(strings.TrimSpace(h))
		if label == want || label == "short context "+want {
			return i
		}
	}
	return -1
}

// pricePerMillion validates a per-million-token price lifted from the page's
// props. Values arrive as JSON numbers; dash placeholders, "Free" and other
// non-numeric strings are rejected, as are zero, negative, NaN and infinite
// values.
func pricePerMillion(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0, false
	}
	return f, true
}

// normalizeSlug converts an OpenAI display name into the canonical model slug
// shared with OpenRouter and LiteLLM. Dots are preserved so "gpt-4.1" stays
// "gpt-4.1"; parenthetical context-length annotations ("gpt-5.5 (<272K context
// length)") are dropped so the row matches its canonical slug.
func normalizeSlug(name string) string {
	if idx := strings.Index(name, "("); idx >= 0 {
		name = name[:idx]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer(" ", "-", "_", "-").Replace(name)
}

// --- Props helpers --------------------------------------------------------

func propString(props map[string]any, key string) (string, bool) {
	v, ok := props[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func propSlice(props map[string]any, key string) ([]any, bool) {
	v, ok := props[key]
	if !ok {
		return nil, false
	}
	s, ok := v.([]any)
	return s, ok
}

// stringSlice converts a decoded array to strings. Non-string entries (some
// headings carry tooltip objects) become empty strings rather than failing the
// whole table.
func stringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

// --- HTML helpers ---------------------------------------------------------

// attrValue returns the value of the named attribute, or "" if absent.
func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// isHeadingLike returns true if n is a span/div/p whose class attribute
// contains "heading" — the class OpenAI uses for several pricing-section
// labels. Actual <h1>–<h4> elements are handled directly by the walker and
// never reach this function.
func isHeadingLike(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	return strings.Contains(attrValue(n, "class"), "heading")
}

// cleanHeading normalises heading text for section comparison by stripping
// decorative icon glyphs (private-use area, zero-width characters, variation
// selectors) that some docs sites render inside heading anchors.
func cleanHeading(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case r >= 0xE000 && r <= 0xF8FF: // private use area (icon fonts)
			return -1
		case r >= 0x200B && r <= 0x200D: // zero-width space / joiner / non-joiner
			return -1
		case r == 0xFE0E || r == 0xFE0F: // text / emoji variation selectors
			return -1
		default:
			return r
		}
	}, s))
}

// textContent returns the concatenated text of all text nodes under n,
// stripping all HTML tags.
func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(n)
	return strings.TrimSpace(sb.String())
}
