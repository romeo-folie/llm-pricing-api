package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultAmazonURL = "https://aws.amazon.com/bedrock/pricing/"

// AmazonScraper fetches model pricing from the Amazon Bedrock pricing page daily.
// The Bedrock pricing table has four columns: Provider, Model ID, Input, Output.
// Provider is extracted from the Provider column and lowercased.
type AmazonScraper struct {
	client *http.Client
	url    string
}

// NewAmazon returns an AmazonScraper using the provided HTTP client.
// If client is nil a default client with a 30s timeout is used.
func NewAmazon(client *http.Client) *AmazonScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AmazonScraper{client: client, url: defaultAmazonURL}
}

// Fetch retrieves models from the Amazon Bedrock pricing page.
func (s *AmazonScraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	body, err := fetchHTML(ctx, s.client, s.url, "amazon-docs")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	// Amazon Bedrock table: Provider(0), Model ID(1), Input(2), Output(3)
	cfg := parseTableConfig{modelCol: 1, providerCol: 0, inputCol: 2, outputCol: 3}
	rows, err := parsePricingTable(body, cfg)
	if err != nil {
		slog.Warn("amazon-docs: failed to parse pricing table", "err", err)
		return nil, fmt.Errorf("amazon-docs: parse: %w", err)
	}
	if len(rows) == 0 {
		slog.Warn("amazon-docs: pricing table returned no models", "url", s.url)
		return nil, fmt.Errorf("amazon-docs: parse: no models found (page layout may have changed)")
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(rows))
	for _, r := range rows {
		models = append(models, scraper.ScrapedModel{
			Slug:               r.slug,
			Provider:           r.provider, // lowercased from Provider column
			InputCostPerToken:  r.input,
			OutputCostPerToken: r.output,
			Modality:           "text",
			SourceName:         "amazon-docs",
			FetchedAt:          fetchedAt,
		})
	}
	return models, nil
}
