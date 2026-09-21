// Package anthropic implements an HTML scraper for the Anthropic Claude
// pricing page.
//
// The page at https://platform.claude.com/docs/en/about-claude/pricing is
// server-side rendered — pricing tables are available in the raw HTML with
// no JavaScript execution required.
//
// Prices on the page are expressed in USD per million tokens (MTok).
// This scraper converts them to per-token cost before returning ScrapedModels:
//
//	page price ($/MTok) ÷ 1,000,000 → InputCostPerToken / OutputCostPerToken
package anthropic

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"llm-pricing-api/internal/scraper"
)

const defaultURL = "https://platform.claude.com/docs/en/about-claude/pricing"

// Scraper fetches model pricing from the Anthropic Claude pricing page.
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

// PricingTable is the intermediate, unvalidated representation of one HTML
// table extracted from the Anthropic pricing page.
type PricingTable struct {
	// Section is the h2/h3 heading that preceded this table on the page
	// (e.g. "Model pricing", "Batch processing").
	Section string
	// Headers holds the column header texts (first column excluded).
	Headers []string
	// Rows holds one entry per data row.  Row[0] is the model name;
	// remaining elements are the raw price strings ("$5 / MTok", etc.).
	Rows [][]string
}

// --- Fetch ----------------------------------------------------------------

// Fetch retrieves pricing data from the Anthropic pricing page, parses the
// HTML, and returns one ScrapedModel per model that carries a numeric Input
// price and Output price expressed in USD per token.
//
// Only the "Model pricing" section (standard per-token rates) is converted to
// ScrapedModels. Batch, long-context, tool-use, and cache-multiplier tables
// are skipped because their price units or structure differ.
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("anthropic: unexpected status", "status", resp.StatusCode, "body_excerpt", string(buf[:n]))
		return nil, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	tables, err := parseHTML(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: parse html: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := toScrapedModels(tables, fetchedAt)

	if len(models) == 0 {
		return nil, fmt.Errorf("anthropic: no models returned after filtering — page structure may have changed")
	}

	slog.Debug("anthropic: scraped models", "count", len(models), "tables", len(tables))
	return models, nil
}

// --- HTML parser ----------------------------------------------------------

// parseHTML walks the HTML document and extracts all pricing tables, tagging
// each with the nearest preceding h2/h3 heading.
func parseHTML(r io.Reader) ([]PricingTable, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var tables []PricingTable
	currentSection := ""

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4":
				// Headings can carry decorative, non-text children (the docs
				// site renders a copy-link button whose icon is a private-use
				// glyph). cleanHeading strips those so the section label stays
				// comparable to the literal strings used below.
				if text := cleanHeading(textContent(n)); text != "" {
					currentSection = text
				}
			case "table":
				if pt, ok := extractTable(n, currentSection); ok {
					tables = append(tables, pt)
				}
				return // don't descend into table — extractTable handles it
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return tables, nil
}

// extractTable parses a <table> node into a PricingTable.
func extractTable(n *html.Node, section string) (PricingTable, bool) {
	pt := PricingTable{Section: section}

	var thead, tbody *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			switch c.Data {
			case "thead":
				thead = c
			case "tbody":
				tbody = c
			}
		}
	}
	if thead == nil || tbody == nil {
		return pt, false
	}

	pt.Headers = extractHeaders(thead)
	if len(pt.Headers) == 0 {
		return pt, false
	}

	pt.Rows = extractRows(tbody)
	if len(pt.Rows) == 0 {
		return pt, false
	}

	return pt, true
}

// extractHeaders returns column headers (first column excluded) from <thead>.
func extractHeaders(thead *html.Node) []string {
	var rows []*html.Node
	var collectRows func(*html.Node)
	collectRows = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			rows = append(rows, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectRows(c)
		}
	}
	collectRows(thead)
	if len(rows) == 0 {
		return nil
	}

	var headers []string
	first := true
	for th := rows[len(rows)-1].FirstChild; th != nil; th = th.NextSibling {
		if th.Type != html.ElementNode || th.Data != "th" {
			continue
		}
		if first {
			first = false
			continue
		}
		text := strings.TrimSpace(textContent(th))
		span := colSpan(th)
		for i := 0; i < span; i++ {
			headers = append(headers, text)
		}
	}
	return headers
}

// extractRows returns all data rows from <tbody>.
func extractRows(tbody *html.Node) [][]string {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			row := extractCells(n)
			if len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(tbody)
	return rows
}

// extractCells returns the text of each <td>/<th> in a <tr>.
func extractCells(tr *html.Node) []string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, strings.TrimSpace(textContent(c)))
		}
	}
	return cells
}

// --- Conversion -----------------------------------------------------------

// toScrapedModels converts parsed PricingTables to ScrapedModels.
// Only the "Model pricing" section with "Base Input Tokens" and "Output Tokens"
// columns is converted; all other sections are skipped.
func toScrapedModels(tables []PricingTable, fetchedAt time.Time) []scraper.ScrapedModel {
	var models []scraper.ScrapedModel
	seen := make(map[string]bool)

	for _, pt := range tables {
		if !strings.EqualFold(pt.Section, "Model pricing") {
			slog.Debug("anthropic: skipping section", "section", pt.Section)
			continue
		}

		inputIdx := columnIndex(pt.Headers, "Base Input")
		outputIdx := columnIndex(pt.Headers, "Output")
		if inputIdx < 0 || outputIdx < 0 {
			slog.Debug("anthropic: skipping table without input/output columns",
				"section", pt.Section, "headers", pt.Headers)
			continue
		}

		for _, row := range pt.Rows {
			if len(row) == 0 {
				continue
			}
			// Strip "(deprecated)" suffix from model names.
			modelName := cleanModelName(row[0])
			if modelName == "" {
				continue
			}

			// Row cells are the model name plus one cell per header. Anything
			// else means a cell was merged or dropped, which would shift the
			// input/output indices onto a cache or long-context column — skip
			// rather than risk publishing the wrong price.
			inIdx := inputIdx + 1
			outIdx := outputIdx + 1
			if len(row) != len(pt.Headers)+1 || inIdx >= len(row) || outIdx >= len(row) {
				slog.Debug("anthropic: skipping row — cell count does not match headers",
					"model", modelName, "cells", len(row), "headers", len(pt.Headers))
				continue
			}

			inputPrice, err := parsePricePerMTok(row[inIdx])
			if err != nil {
				slog.Debug("anthropic: skipping row — cannot parse input price",
					"model", modelName, "raw", row[inIdx])
				continue
			}
			outputPrice, err := parsePricePerMTok(row[outIdx])
			if err != nil {
				slog.Debug("anthropic: skipping row — cannot parse output price",
					"model", modelName, "raw", row[outIdx])
				continue
			}

			slug := "anthropic/" + canonicalAnthropicSlug(modelName)
			if seen[slug] {
				continue
			}
			seen[slug] = true

			models = append(models, scraper.ScrapedModel{
				Slug:               slug,
				Provider:           "anthropic",
				InputCostPerToken:  inputPrice,
				OutputCostPerToken: outputPrice,
				Modality:           "text",
				SourceName:         "anthropic",
				FetchedAt:          fetchedAt,
			})
		}
	}

	return models
}

// columnIndex returns the 0-based index of the first header containing text.
func columnIndex(headers []string, text string) int {
	lower := strings.ToLower(text)
	for i, h := range headers {
		if strings.Contains(strings.ToLower(h), lower) {
			return i
		}
	}
	return -1
}

// parsePricePerMTok parses Anthropic's "$X / MTok" price format and converts
// to per-token cost by dividing by 1,000,000.
// Returns an error for dashes, empty strings, and non-numeric values.
func parsePricePerMTok(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "—" {
		return 0, fmt.Errorf("no price: %q", s)
	}
	// Strip "$" prefix and everything from "/" onwards (e.g. "/ MTok").
	s = strings.TrimPrefix(s, "$")
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", s, err)
	}
	// ParseFloat accepts "NaN", "Inf" and "-Inf". Reject them explicitly:
	// NaN compares false against `v <= 0`, so without this guard a non-finite
	// value would be divided and stored as a price.
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("non-finite price: %q", s)
	}
	if v <= 0 {
		return 0, fmt.Errorf("non-positive price: %v", v)
	}
	return v / 1_000_000, nil
}

// cleanModelName strips trailing annotations like "(deprecated)" and trims
// surrounding whitespace.
func cleanModelName(s string) string {
	s = strings.TrimSpace(s)
	// Remove parenthetical suffixes.
	for _, suffix := range []string{"(deprecated)", "(legacy)", "(preview)"} {
		s = strings.TrimSuffix(strings.TrimSpace(s), suffix)
	}
	return strings.TrimSpace(s)
}

// canonicalAnthropicSlug converts a human-facing Anthropic model name into the
// canonical slug format used throughout the repo:
//
//	"Claude Opus 4.6"   → "claude-4-6-opus"
//	"Claude Sonnet 4.5" → "claude-4-5-sonnet"
//	"Claude Haiku 3.5"  → "claude-3-5-haiku"
//
// The slug format produced is `claude-<major>-<minor>-<variant>` (version
// first, variant last), e.g. "Claude Opus 4.6" → "claude-4-6-opus".
// Note: this scraper omits date suffixes (the pricing page does not expose
// release dates). Historical hand-curated slugs that do carry dates (e.g.
// `claude-3-5-sonnet-20241022`) continue to exist in aliases.go and are
// unaffected; the scraper produces separate dated-free rows for any model
// whose date-suffixed alias has not been added yet.
//
// The scraper previously emitted `claude-opus-4-6` (variant first), which
// would not match or reconcile against any existing rows.
//
// Parsing is deterministic for the "Claude <Variant> <Major>.<Minor>" pattern.
// If parsing fails (unrecognised name format), the function falls back to a
// simple lowercase-with-hyphens slug so unknown future models are still stored
// rather than silently dropped.
func canonicalAnthropicSlug(name string) string {
	// Strip parenthetical suffixes like "(deprecated)" or "(beta)".
	if idx := strings.Index(name, "("); idx != -1 {
		name = strings.TrimSpace(name[:idx])
	}

	// Expected formats:
	//   "Claude <Variant> <Major>.<Minor>"  → claude-<major>-<minor>-<variant>
	//   "Claude <Variant> <Major>"          → claude-<major>-<variant>
	// Split on spaces; token[0]="Claude", token[1]=variant, token[2]=version.
	// The version must start with a digit: without that check a version-first
	// name such as "Claude 3.5 Sonnet" would be read as variant "3.5" and
	// version "Sonnet", producing a bogus slug. Version-first names fall
	// through to the lowercase-hyphenated fallback below.
	parts := strings.Fields(name)
	if len(parts) == 3 && strings.EqualFold(parts[0], "claude") && startsWithDigit(parts[2]) {
		variant := strings.ToLower(parts[1])
		version := parts[2] // e.g. "4.6", "3.5", or "4"
		verParts := strings.SplitN(version, ".", 2)
		if len(verParts) == 2 {
			// SplitN with n=2 guarantees verParts[0] has no dots;
			// verParts[1] is the remainder (e.g. "5" from "3.5").
			// Replace any residual dots in the minor segment with
			// hyphens to handle hypothetical "3.5.1" → "3-5-1".
			minor := strings.ReplaceAll(verParts[1], ".", "-")
			return fmt.Sprintf("claude-%s-%s-%s", verParts[0], minor, variant)
		}
		// Major-only version (no dot): "Claude Opus 4" → "claude-4-opus"
		if len(verParts) == 1 && verParts[0] != "" {
			return fmt.Sprintf("claude-%s-%s", verParts[0], variant)
		}
	}

	// Fallback: lowercase + replace separators.
	name = strings.ToLower(name)
	return strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(name)
}

// startsWithDigit reports whether s begins with an ASCII digit.
func startsWithDigit(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

// --- HTML helpers ---------------------------------------------------------

// cleanHeading normalises a heading's text for section comparison.
//
// The Anthropic docs site appends decorative icon glyphs to headings — most
// notably U+E09A, the private-use character inside the "copy link" button's
// icon span. textContent collects it, so without stripping, the section label
// reads "Model pricing\ue09a" and never matches the literal "Model pricing".
// That mismatch silently disabled this scraper for ~190 days (#209).
//
// Stripped: the Unicode private-use area (U+E000–U+F8FF), zero-width
// characters, and text/emoji variation selectors. Ordinary text — including
// non-ASCII model names — is preserved.
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

// textContent returns the concatenated text of all text nodes under n.
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

// colSpan returns the integer colspan value of n, defaulting to 1.
func colSpan(n *html.Node) int {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "colspan") {
			if v, err := strconv.Atoi(a.Val); err == nil && v > 0 {
				return v
			}
		}
	}
	return 1
}
