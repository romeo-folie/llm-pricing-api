package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultAnthropicURL = "https://www.anthropic.com/pricing"

// AnthropicScraper fetches model pricing from the Anthropic pricing page daily.
type AnthropicScraper struct {
	client *http.Client
	url    string
}

// NewAnthropic returns an AnthropicScraper using the provided HTTP client.
// If client is nil a default client with a 30s timeout is used.
func NewAnthropic(client *http.Client) *AnthropicScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AnthropicScraper{client: client, url: defaultAnthropicURL}
}

// Fetch retrieves models from the Anthropic pricing page.
func (s *AnthropicScraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	body, err := fetchHTML(ctx, s.client, s.url, "anthropic-docs")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	cfg := parseTableConfig{modelCol: 0, providerCol: -1, inputCol: 1, outputCol: 2}
	rows, err := parsePricingTable(body, cfg)
	if err != nil {
		slog.Warn("anthropic-docs: failed to parse pricing table", "err", err)
		return nil, fmt.Errorf("anthropic-docs: parse: %w", err)
	}
	if len(rows) == 0 {
		slog.Warn("anthropic-docs: pricing table returned no models", "url", s.url)
		return nil, fmt.Errorf("anthropic-docs: parse: no models found (page layout may have changed)")
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(rows))
	for _, r := range rows {
		models = append(models, scraper.ScrapedModel{
			Slug:               r.slug,
			Provider:           "anthropic",
			InputCostPerToken:  r.input,
			OutputCostPerToken: r.output,
			Modality:           "text",
			SourceName:         "anthropic-docs",
			FetchedAt:          fetchedAt,
		})
	}
	return models, nil
}
