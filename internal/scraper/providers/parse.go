package providers

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// priceRow holds one parsed row from an HTML pricing table.
type priceRow struct {
	slug     string
	provider string // only populated by scrapers that have a dedicated Provider column
	input    float64
	output   float64
}

// parseTableConfig controls which columns the parser reads.
type parseTableConfig struct {
	// modelCol is the 0-based index of the column holding the model name/slug.
	modelCol int
	// providerCol, when >= 0, is the column holding the provider name.
	providerCol int
	// inputCol is the 0-based index of the input price column.
	inputCol int
	// outputCol is the 0-based index of the output price column.
	outputCol int
}

// parsePricingTable parses all rows from the first HTML <table> that has at
// least (max column index + 1) columns in its header row. Prices are expected
// to contain a dollar amount like "$2.50" and are normalised from per-1M-tokens
// to per-token by dividing by 1,000,000.
//
// Rows where the price cells cannot be parsed are silently skipped.
func parsePricingTable(r io.Reader, cfg parseTableConfig) ([]priceRow, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("html parse: %w", err)
	}

	// Walk the document to find <table> elements.
	var rows []priceRow
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			rows = append(rows, parseTable(n, cfg)...)
			return // don't recurse into nested tables
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return rows, nil
}

// parseTable extracts priceRows from a single <table> node.
//
// Row selection: only <tr> elements inside a <tbody> are processed; <thead>
// rows are skipped. This relies on golang.org/x/net/html's HTML5-compliant
// tree builder, which inserts an implicit <tbody> element when the raw HTML
// omits it (matching browser behaviour). Raw HTML without explicit <tbody>
// still works correctly because the parser normalises the tree before we walk it.
func parseTable(table *html.Node, cfg parseTableConfig) []priceRow {
	var rows []priceRow
	inBody := false

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "thead":
				inBody = false
			case "tbody":
				inBody = true
			case "tr":
				if inBody {
					if row, ok := parseRow(n, cfg); ok {
						rows = append(rows, row)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	return rows
}

// parseRow extracts a priceRow from a <tr> node. Returns false if the row
// does not have enough columns or prices cannot be parsed.
func parseRow(tr *html.Node, cfg parseTableConfig) (priceRow, bool) {
	cells := collectCells(tr)

	minCols := cfg.modelCol + 1
	if cfg.providerCol >= 0 && cfg.providerCol+1 > minCols {
		minCols = cfg.providerCol + 1
	}
	if cfg.inputCol+1 > minCols {
		minCols = cfg.inputCol + 1
	}
	if cfg.outputCol+1 > minCols {
		minCols = cfg.outputCol + 1
	}
	if len(cells) < minCols {
		return priceRow{}, false
	}

	slug := strings.TrimSpace(cells[cfg.modelCol])
	if slug == "" {
		return priceRow{}, false
	}

	input, err := parseDollarAmount(cells[cfg.inputCol])
	// Skip rows with negative or zero prices. Provider pages occasionally list
	// free-tier / open-weight models as $0.00; these are outside the scope of
	// this platform (Phase 1 focuses on commercially-priced models), consistent
	// with the OpenRouter and LiteLLM scraper policies.
	if err != nil || input <= 0 {
		return priceRow{}, false
	}
	output, err := parseDollarAmount(cells[cfg.outputCol])
	if err != nil || output <= 0 {
		return priceRow{}, false
	}

	var provider string
	if cfg.providerCol >= 0 {
		provider = strings.ToLower(strings.TrimSpace(cells[cfg.providerCol]))
	}

	return priceRow{
		slug:     slug,
		provider: provider,
		// Prices on provider pages are per 1M tokens; normalise to per-token.
		input:  input / 1_000_000,
		output: output / 1_000_000,
	}, true
}

// collectCells returns the text content of each <td> (or <th>) in a <tr>.
func collectCells(tr *html.Node) []string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, textContent(c))
		}
	}
	return cells
}

// textContent returns the concatenated text nodes under n, with whitespace
// normalised to a single space.
func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

// parseDollarAmount extracts the first dollar amount from a string like
// "$2.50 / 1M tokens" or "$10.00/1M" and returns it as a float64.
func parseDollarAmount(s string) (float64, error) {
	// Find the '$' and read digits/decimal following it.
	idx := strings.Index(s, "$")
	if idx < 0 {
		return 0, fmt.Errorf("no $ in %q", s)
	}
	rest := s[idx+1:]
	// Consume digits and at most one decimal point.
	end := 0
	hasDot := false
	for end < len(rest) {
		c := rest[end]
		if c >= '0' && c <= '9' {
			end++
		} else if c == '.' && !hasDot {
			hasDot = true
			end++
		} else {
			break
		}
	}
	if end == 0 {
		return 0, fmt.Errorf("no numeric value after $ in %q", s)
	}
	return strconv.ParseFloat(rest[:end], 64)
}
