package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultOpenAIURL = "https://openai.com/api/pricing/"

// OpenAIScraper fetches model pricing from the OpenAI pricing page daily.
type OpenAIScraper struct {
	client *http.Client
	url    string
}

// NewOpenAI returns an OpenAIScraper using the provided HTTP client.
// If client is nil a default client with a 30s timeout is used.
func NewOpenAI(client *http.Client) *OpenAIScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenAIScraper{client: client, url: defaultOpenAIURL}
}

// Fetch retrieves models from the OpenAI pricing page. Parse errors are logged
// and an empty slice is returned rather than an error, so one failed provider
// does not abort the pipeline.
func (s *OpenAIScraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	body, err := fetchHTML(ctx, s.client, s.url, "openai-docs")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	cfg := parseTableConfig{modelCol: 0, providerCol: -1, inputCol: 1, outputCol: 2}
	rows, err := parsePricingTable(body, cfg)
	if err != nil {
		slog.Warn("openai-docs: failed to parse pricing table", "err", err)
		return nil, fmt.Errorf("openai-docs: parse: %w", err)
	}
	if len(rows) == 0 {
		slog.Warn("openai-docs: pricing table returned no models", "url", s.url)
		return nil, fmt.Errorf("openai-docs: parse: no models found (page layout may have changed)")
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(rows))
	for _, r := range rows {
		models = append(models, scraper.ScrapedModel{
			Slug:               r.slug,
			Provider:           "openai",
			InputCostPerToken:  r.input,
			OutputCostPerToken: r.output,
			Modality:           "text",
			SourceName:         "openai-docs",
			FetchedAt:          fetchedAt,
		})
	}
	return models, nil
}
