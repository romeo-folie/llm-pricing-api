package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultMistralURL = "https://mistral.ai/technology/#pricing"

// MistralScraper fetches model pricing from the Mistral AI pricing page daily.
type MistralScraper struct {
	client *http.Client
	url    string
}

// NewMistral returns a MistralScraper using the provided HTTP client.
// If client is nil a default client with a 30s timeout is used.
func NewMistral(client *http.Client) *MistralScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &MistralScraper{client: client, url: defaultMistralURL}
}

// Fetch retrieves models from the Mistral AI pricing page.
func (s *MistralScraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	body, err := fetchHTML(ctx, s.client, s.url, "mistral-docs")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	cfg := parseTableConfig{modelCol: 0, providerCol: -1, inputCol: 1, outputCol: 2}
	rows, err := parsePricingTable(body, cfg)
	if err != nil {
		slog.Warn("mistral-docs: failed to parse pricing table", "err", err)
		return nil, fmt.Errorf("mistral-docs: parse: %w", err)
	}
	if len(rows) == 0 {
		slog.Warn("mistral-docs: pricing table returned no models", "url", s.url)
		return nil, fmt.Errorf("mistral-docs: parse: no models found (page layout may have changed)")
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(rows))
	for _, r := range rows {
		models = append(models, scraper.ScrapedModel{
			Slug:               r.slug,
			Provider:           "mistral",
			InputCostPerToken:  r.input,
			OutputCostPerToken: r.output,
			Modality:           "text",
			SourceName:         "mistral-docs",
			FetchedAt:          fetchedAt,
		})
	}
	return models, nil
}
