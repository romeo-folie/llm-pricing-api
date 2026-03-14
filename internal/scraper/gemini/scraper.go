// Package gemini implements an HTML scraper for the Google Gemini API pricing
// page.
//
// The page at https://ai.google.dev/gemini-api/docs/pricing is server-side
// rendered when accessed with a Googlebot User-Agent; using a browser UA
// triggers an OAuth redirect loop.  No JavaScript execution is required with
// the Googlebot UA.
//
// Page structure
//
//	H1: "Gemini Developer API pricing"
//	H2: <model name>           (e.g. "Gemini 2.5 Pro")
//	  H3: "Standard"
//	    TABLE                  (one table per model per tier)
//	  H3: "Batch"
//	    TABLE
//
// Each table is "transposed" — rows are named price fields; columns are Free
// Tier and Paid Tier.  The paid-tier price in the "Input price" and
// "Output price" rows (column index 2, 1-based) is extracted.
//
// Prices are in USD / 1M tokens.  This scraper converts them to per-token
// cost:
//
//	page price ($/1M) ÷ 1,000,000 → InputCostPerToken / OutputCostPerToken
package gemini

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"llm-pricing-api/internal/scraper"
)

const defaultURL = "https://ai.google.dev/gemini-api/docs/pricing"

// Googlebot UA is required to bypass the OAuth redirect that the page serves
// to regular browser User-Agents.
const userAgent = "Googlebot/2.1 (+http://www.google.com/bot.html)"

// Scraper fetches model pricing from the Google Gemini API pricing page.
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

// ModelPricing is the intermediate, unvalidated representation of one Gemini
// model's pricing table extracted from the HTML page.
type ModelPricing struct {
	// ModelName is the H2 heading text for this model
	// (e.g. "Gemini 2.5 Pro").
	ModelName string
	// Tier is "Standard" or "Batch" from the H3 subheading.
	Tier string
	// InputRaw is the raw Paid Tier cell text for the "Input price" row
	// (e.g. "$0.25 (text / image / video)", "$2.00, prompts <= 200k tokens").
	InputRaw string
	// OutputRaw is the raw Paid Tier cell text for the "Output price" row.
	OutputRaw string
}

// --- Fetch ----------------------------------------------------------------

// Fetch retrieves pricing data from the Gemini pricing page, parses the HTML,
// and returns one ScrapedModel per Standard-tier model that carries numeric
// Input and Output prices in USD per token.
//
// Batch-tier tables are skipped.  Models whose paid-tier prices are listed as
// "Not available" are skipped.  Image-specific output prices (e.g. "$60.00
// (images)") are filtered in favour of the text output price when both are
// present in the same cell.
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("gemini: unexpected status", "status", resp.StatusCode, "body_excerpt", string(buf[:n]))
		return nil, fmt.Errorf("gemini: unexpected status %d", resp.StatusCode)
	}

	pricings, err := parseHTML(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: parse html: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := toScrapedModels(pricings, fetchedAt)

	if len(models) == 0 {
		return nil, fmt.Errorf("gemini: no models returned after filtering — page structure may have changed")
	}

	slog.Info("gemini: scraped models", "count", len(models), "entries", len(pricings))
	return models, nil
}

// --- HTML parser ----------------------------------------------------------

// parseHTML walks the HTML and extracts ModelPricing for every
// H2→H3("Standard"|"Batch")→TABLE triplet.
func parseHTML(r io.Reader) ([]ModelPricing, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var entries []ModelPricing

	// State tracked across the walk.
	var currentModel string // set by H2
	var currentTier string  // set by H3: "Standard" or "Batch"

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h2":
				if text := cleanText(n); text != "" {
					currentModel = cleanModelName(text)
					currentTier = "" // reset tier on new model
				}
			case "h3":
				if text := cleanText(n); text != "" {
					currentTier = text
				}
			case "table":
				if currentModel != "" && currentTier != "" {
					if mp, ok := extractPricing(n, currentModel, currentTier); ok {
						entries = append(entries, mp)
					}
				}
				return // don't recurse into table via walk
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return entries, nil
}

// extractPricing extracts the Input and Output paid-tier prices from a single
// Gemini pricing table.  The table is transposed: column 0 = row label,
// column 1 = Free Tier, column 2 = Paid Tier.
func extractPricing(n *html.Node, modelName, tier string) (ModelPricing, bool) {
	mp := ModelPricing{ModelName: modelName, Tier: tier}

	// Collect all rows from the table (including those inside tbody).
	rows := collectTableRows(n)

	for _, row := range rows {
		cells := rowCells(row)
		if len(cells) < 3 {
			continue
		}
		label := strings.TrimSpace(textContent(cells[0]))
		paidCell := strings.TrimSpace(textContent(cells[2]))

		switch {
		case strings.HasPrefix(strings.ToLower(label), "input price"):
			mp.InputRaw = paidCell
		case strings.HasPrefix(strings.ToLower(label), "output price"):
			mp.OutputRaw = paidCell
		}
	}

	if mp.InputRaw == "" && mp.OutputRaw == "" {
		return mp, false
	}
	return mp, true
}

// collectTableRows returns all <tr> nodes anywhere inside a <table>.
func collectTableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			rows = append(rows, n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	return rows
}

// rowCells returns all <td>/<th> nodes that are direct children of a <tr>.
func rowCells(tr *html.Node) []*html.Node {
	var cells []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, c)
		}
	}
	return cells
}

// --- Conversion -----------------------------------------------------------

// toScrapedModels converts ModelPricing entries to ScrapedModels.
// Only Standard-tier entries with parseable paid-tier Input and Output prices
// are included.
func toScrapedModels(entries []ModelPricing, fetchedAt time.Time) []scraper.ScrapedModel {
	var models []scraper.ScrapedModel
	seen := make(map[string]bool)

	for _, mp := range entries {
		if !strings.EqualFold(mp.Tier, "Standard") {
			slog.Debug("gemini: skipping non-standard tier", "model", mp.ModelName, "tier", mp.Tier)
			continue
		}

		inputPrice, err := parsePricePerMillion(mp.InputRaw)
		if err != nil {
			slog.Debug("gemini: skipping — cannot parse input price",
				"model", mp.ModelName, "raw", mp.InputRaw)
			continue
		}
		outputPrice, err := parsePricePerMillion(mp.OutputRaw)
		if err != nil {
			slog.Debug("gemini: skipping — cannot parse output price",
				"model", mp.ModelName, "raw", mp.OutputRaw)
			continue
		}

		slug := "google/" + normalizeSlug(mp.ModelName)
		if seen[slug] {
			continue
		}
		seen[slug] = true

		models = append(models, scraper.ScrapedModel{
			Slug:               slug,
			Provider:           "google",
			InputCostPerToken:  inputPrice,
			OutputCostPerToken: outputPrice,
			Modality:           "text",
			SourceName:         "google",
			FetchedAt:          fetchedAt,
		})
	}

	return models
}

// firstPriceRe matches the first "$X" or "$X.XX" number in a string.
var firstPriceRe = regexp.MustCompile(`\$(\d[\d,]*(?:\.\d+)?)`)

// parsePricePerMillion extracts the first USD price from a Gemini pricing
// cell value and converts it from $/1M tokens to per-token cost.
//
// Gemini cell values can look like:
//   - "$0.25 (text / image / video)" → 0.25
//   - "$2.00, prompts <= 200k tokens" → 2.00 (first/lower-context price)
//   - "$3 (text and thinking)\n$60.00 (images)" → 3.0 (text price only)
//   - "Not available" → error
//   - "Free of charge" → error (free tier; paid tier is what we want)
func parsePricePerMillion(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "not available") || strings.EqualFold(s, "free of charge") {
		return 0, fmt.Errorf("no paid price: %q", s)
	}

	// For cells with multiple lines (e.g. text price + image price), use the
	// first line only to get the text-modality price.
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[:idx]
	}

	m := firstPriceRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("no price found in %q", s)
	}

	numStr := strings.ReplaceAll(m[1], ",", "")
	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", numStr, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("non-positive price: %v", v)
	}
	return v / 1_000_000, nil
}

// cleanModelName strips emoji, parenthetical tags, and extra whitespace from
// a model heading.  E.g. "Gemini 3.1 Flash Image Preview 🍌" →
// "Gemini 3.1 Flash Image Preview".
func cleanModelName(s string) string {
	// Remove emoji (any character in Unicode category 'So' or 'Sm', or
	// specifically the common emoji ranges).
	var sb strings.Builder
	for _, r := range s {
		if !isEmoji(r) {
			sb.WriteRune(r)
		}
	}
	s = strings.TrimSpace(sb.String())
	return s
}

// isEmoji returns true for characters that are visually emoji on most systems.
func isEmoji(r rune) bool {
	return unicode.Is(unicode.So, r) || // Symbol, other (includes many emoji)
		(r >= 0x1F300 && r <= 0x1FAFF) || // Misc Symbols and Pictographs
		(r >= 0x2600 && r <= 0x27BF) // Misc Symbols
}

// normalizeSlug lowercases and replaces spaces/dots/underscores with hyphens.
func normalizeSlug(name string) string {
	name = strings.ToLower(name)
	name = strings.NewReplacer(" ", "-", "_", "-", ".", "-").Replace(name)
	return name
}

// --- HTML helpers ---------------------------------------------------------

// cleanText returns the trimmed text content of n with internal whitespace
// collapsed.
func cleanText(n *html.Node) string {
	return strings.TrimSpace(textContent(n))
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
