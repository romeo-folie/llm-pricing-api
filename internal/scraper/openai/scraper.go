// Package openai implements an HTML scraper for the OpenAI API pricing page.
//
// The OpenAI pricing page at https://developers.openai.com/api/docs/pricing is
// statically rendered (SSR/SSG via Astro), so no JavaScript execution is
// required. Pricing tables are present in the raw HTML and can be parsed
// using a standard HTML parser.
//
// Prices on the page are expressed in USD per 1 million tokens.
// This scraper converts them to per-token cost before returning ScrapedModels:
//
//	page price ($/1M) ÷ 1,000,000 → InputCostPerToken / OutputCostPerToken
package openai

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"llm-pricing-api/internal/scraper"
)

// defaultURL is the canonical OpenAI API pricing page.  This must match the
// URL stored in the sources table (migrations/000009_update_openai_source_url).
const defaultURL = "https://developers.openai.com/api/docs/pricing?latest-pricing=standard"

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

// PricingTable is the intermediate, unvalidated representation of one HTML
// table extracted from the OpenAI pricing page.  It is produced by parseHTML
// and consumed by toScrapedModels.
type PricingTable struct {
	// Section is the human-readable section label that preceded this table
	// on the page (e.g. "Text tokens", "Image tokens", "Fine-tuning").
	Section string
	// Headers holds the column header texts after expanding colSpan and
	// stripping HTML.  The first column (model name) is excluded.
	Headers []string
	// Rows holds one entry per data row.  Row[0] is the model name; the
	// remaining elements are the raw price strings ("$1.25", "-", etc.)
	// and are parallel to Headers.
	Rows [][]string
}

// --- Fetch ----------------------------------------------------------------

// Fetch retrieves pricing data from the OpenAI pricing page, parses the HTML,
// and returns one ScrapedModel per model that carries a numeric Input price
// and Output price expressed in USD per token.
//
// Models from sections other than "Text tokens" (e.g. image, video, audio)
// are skipped because their price units differ from per-token costs and
// cannot be stored in ScrapedModel.InputCostPerToken / OutputCostPerToken.
// Fine-tuning rows are skipped for the same reason (they carry a training
// hourly rate in addition to inference prices).
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

	tables, err := parseHTML(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: parse html: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := toScrapedModels(tables, fetchedAt)

	if len(models) == 0 {
		return nil, fmt.Errorf("openai: no models returned after filtering — page structure may have changed")
	}

	slog.Info("openai: scraped models", "count", len(models), "tables", len(tables))
	return models, nil
}

// --- HTML parser ----------------------------------------------------------

// parseHTML walks the HTML document and extracts all pricing tables, tagging
// each with the nearest preceding section label.
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
			case "h1", "h2", "h3", "h4", "span", "div", "p":
				// Section labels on the OpenAI pricing page are rendered as
				// <span class="...heading..."> elements.  h1–h4 are also
				// matched here so that isHeadingLike can filter by class for
				// non-heading tags, and future page restructuring around real
				// heading elements is handled automatically.
				if isHeadingLike(n) {
					if text := textContent(n); text != "" {
						currentSection = text
					}
				}
			case "table":
				if pt, ok := extractTable(n, currentSection); ok {
					tables = append(tables, pt)
				}
				return // don't descend into table via walk — extractTable handles it
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return tables, nil
}

// isHeadingLike returns true if n is a short inline/block element whose text
// could be a pricing section label.  We check that:
//   - it has a class attribute containing "heading" (OpenAI's CSS class), OR
//   - it is an <h1>–<h4> element
func isHeadingLike(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	tag := n.Data
	if tag == "h1" || tag == "h2" || tag == "h3" || tag == "h4" {
		return true
	}
	for _, a := range n.Attr {
		if a.Key == "class" && strings.Contains(a.Val, "heading") {
			return true
		}
	}
	return false
}

// extractTable parses a <table> node into a PricingTable.
// Returns (table, true) on success or (zero, false) if the table cannot be
// meaningfully parsed (no thead, no tbody, empty headers, or no data rows).
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

	// Parse headers — expand colSpan, skip the first (model name) column.
	pt.Headers = extractHeaders(thead)
	if len(pt.Headers) == 0 {
		return pt, false
	}

	// Parse data rows.
	pt.Rows = extractRows(tbody)
	if len(pt.Rows) == 0 {
		return pt, false
	}

	return pt, true
}

// extractHeaders returns the column headers (excluding the model-name column)
// from the last <tr> inside <thead>, expanding colSpan repetitions.
//
// Only the last header row is used for column names.  Earlier rows (e.g.
// group-label rows like "Short context / Long context") are intentionally
// ignored: their colspan attributes span pricing-column groups, not individual
// columns, and incorporating them would require building a full multi-row
// header merge — complexity not justified for the current page structure.
// If OpenAI adds tiered-context columns that only appear in earlier header
// rows, this function will need to be extended.
func extractHeaders(thead *html.Node) []string {
	// Collect all rows in thead; use the last one (some tables have a
	// multi-row header where the first row holds group labels like
	// "Short context / Long context").
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
	// Use the last header row for column names.
	lastRow := rows[len(rows)-1]

	var headers []string
	first := true // skip the first <th> (model name column)
	for th := lastRow.FirstChild; th != nil; th = th.NextSibling {
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

// extractRows returns all data rows from <tbody>.  Each entry is a string
// slice where [0] is the model name and [1:] are the cell values.
func extractRows(tbody *html.Node) [][]string {
	var rows [][]string
	var walkTbody func(*html.Node)
	walkTbody = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			row := extractCells(n)
			if len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkTbody(c)
		}
	}
	walkTbody(tbody)
	return rows
}

// extractCells returns the text content of each cell in a <tr>.
func extractCells(tr *html.Node) []string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.Data == "td" || c.Data == "th" {
			cells = append(cells, strings.TrimSpace(textContent(c)))
		}
	}
	return cells
}

// --- Conversion to ScrapedModel -------------------------------------------

// sectionModality maps OpenAI page section labels to ScrapedModel.Modality.
var sectionModality = map[string]string{
	"Text tokens":                       "text",
	"Image tokens":                      "image",
	"Audio tokens":                      "audio",
	"Transcription and speech generation": "audio",
}

// toScrapedModels converts parsed PricingTables to ScrapedModels.
// Only tables from "Text tokens" sections with both Input and Output
// per-token prices are included; all other sections are skipped because their
// price units are incompatible with ScrapedModel.InputCostPerToken.
func toScrapedModels(tables []PricingTable, fetchedAt time.Time) []scraper.ScrapedModel {
	var models []scraper.ScrapedModel
	seen := make(map[string]bool) // deduplicate by slug

	for _, pt := range tables {
		modality, ok := sectionModality[pt.Section]
		if !ok {
			slog.Debug("openai: skipping section", "section", pt.Section)
			continue
		}
		// Only text-token tables have per-token Input/Output prices.
		if modality != "text" {
			slog.Debug("openai: skipping non-text section", "section", pt.Section, "modality", modality)
			continue
		}

		inputIdx, outputIdx := columnIndex(pt.Headers, "Input"), columnIndex(pt.Headers, "Output")
		if inputIdx < 0 || outputIdx < 0 {
			slog.Debug("openai: skipping table without Input/Output columns", "section", pt.Section, "headers", pt.Headers)
			continue
		}

		for _, row := range pt.Rows {
			if len(row) == 0 {
				continue
			}
			modelName := row[0]
			if modelName == "" {
				continue
			}

			// Offset by 1 because row[0] is the model name.
			inIdx := inputIdx + 1
			outIdx := outputIdx + 1
			if inIdx >= len(row) || outIdx >= len(row) {
				continue
			}

			inputPrice, err := parsePricePerMillion(row[inIdx])
			if err != nil {
				slog.Debug("openai: skipping row — cannot parse input price", "model", modelName, "raw", row[inIdx])
				continue
			}
			outputPrice, err := parsePricePerMillion(row[outIdx])
			if err != nil {
				slog.Debug("openai: skipping row — cannot parse output price", "model", modelName, "raw", row[outIdx])
				continue
			}

			slug := "openai/" + normalizeSlug(modelName)
			if seen[slug] {
				continue
			}
			seen[slug] = true

			models = append(models, scraper.ScrapedModel{
				Slug:               slug,
				Provider:           "openai",
				InputCostPerToken:  inputPrice,
				OutputCostPerToken: outputPrice,
				Modality:           modality,
				SourceName:         "openai",
				FetchedAt:          fetchedAt,
			})
		}
	}

	return models
}

// columnIndex returns the 0-based index of the first header that exactly
// matches text (case-insensitive, after normalisation).
//
// Exact matching avoids the ambiguity of substring search: "Cached input"
// contains "input", so a Contains match would wrongly pick the cached-input
// column when looking for "Input".  With exact matching the caller gets the
// first column whose full name equals the target.
func columnIndex(headers []string, text string) int {
	target := strings.ToLower(strings.TrimSpace(text))
	for i, h := range headers {
		if strings.ToLower(strings.TrimSpace(h)) == target {
			return i
		}
	}
	return -1
}

// parsePricePerMillion parses a price string like "$1.25" into a per-token
// cost by dividing by 1,000,000.  Returns an error for "-", empty strings,
// and non-numeric values.
func parsePricePerMillion(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "—" {
		return 0, fmt.Errorf("no price: %q", s)
	}
	s = strings.TrimPrefix(s, "$")
	// Strip commas (e.g. "$1,000.00")
	s = strings.ReplaceAll(s, ",", "")
	// Some cells contain additional text after the number (e.g. "/ hour")
	if idx := strings.IndexAny(s, " /"); idx >= 0 {
		s = s[:idx]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", s, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("non-positive price: %v", v)
	}
	// OpenAI page prices are per 1 million tokens.
	return v / 1_000_000, nil
}

// normalizeSlug lowercases and replaces spaces/underscores/dots with hyphens.
func normalizeSlug(name string) string {
	name = strings.ToLower(name)
	name = strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(name)
	return name
}

// --- HTML helpers ---------------------------------------------------------

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

// colSpan returns the integer value of the colspan attribute of n, defaulting
// to 1 if absent or unparseable.
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
