package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultGoogleURL = "https://ai.google.dev/pricing"

// GoogleScraper fetches model pricing from the Google AI pricing page daily.
type GoogleScraper struct {
	client *http.Client
	url    string
}

// NewGoogle returns a GoogleScraper using the provided HTTP client.
// If client is nil a default client with a 30s timeout is used.
func NewGoogle(client *http.Client) *GoogleScraper {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GoogleScraper{client: client, url: defaultGoogleURL}
}

// Fetch retrieves models from the Google AI (Gemini API) pricing page.
func (s *GoogleScraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	body, err := fetchHTML(ctx, s.client, s.url, "google-docs")
	if err != nil {
		return nil, err
	}
	defer body.Close()

	cfg := parseTableConfig{modelCol: 0, providerCol: -1, inputCol: 1, outputCol: 2}
	rows, err := parsePricingTable(body, cfg)
	if err != nil {
		slog.Warn("google-docs: failed to parse pricing table", "err", err)
		return nil, fmt.Errorf("google-docs: parse: %w", err)
	}
	if len(rows) == 0 {
		slog.Warn("google-docs: pricing table returned no models", "url", s.url)
		return nil, fmt.Errorf("google-docs: parse: no models found (page layout may have changed)")
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(rows))
	for _, r := range rows {
		models = append(models, scraper.ScrapedModel{
			Slug:               r.slug,
			Provider:           "google",
			InputCostPerToken:  r.input,
			OutputCostPerToken: r.output,
			Modality:           "text",
			SourceName:         "google-docs",
			FetchedAt:          fetchedAt,
		})
	}
	return models, nil
}
