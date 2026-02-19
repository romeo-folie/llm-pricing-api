package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

// Scraper fetches model pricing from the OpenRouter REST API every 6 hours.
type Scraper struct {
	client  *http.Client
	baseURL string
}

// New returns a Scraper using the provided HTTP client.
// If client is nil a default client with a 15s timeout is used.
func New(client *http.Client) *Scraper {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Scraper{client: client, baseURL: defaultBaseURL}
}

// openRouterResponse is the top-level JSON envelope from GET /v1/models.
type openRouterResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID            string            `json:"id"`
	Pricing       openRouterPricing `json:"pricing"`
	ContextLength int               `json:"context_length"`
	Architecture  openRouterArch    `json:"architecture"`
}

type openRouterPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type openRouterArch struct {
	Modality string `json:"modality"`
}

// Fetch retrieves all models from OpenRouter and normalises them to ScrapedModel.
// Models with missing or zero prices are intentionally skipped: OpenRouter represents
// free/open-weight models with price "0", which are outside the scope of this platform
// (Phase 1 focuses on commercially-priced models where reconciliation is meaningful).
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	req.Header.Set("User-Agent", "llm-pricing-api/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("openrouter: unexpected status", "status", resp.StatusCode, "body_excerpt", string(buf[:n]))
		return nil, fmt.Errorf("openrouter: unexpected status %d", resp.StatusCode)
	}

	var raw openRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("openrouter: decode: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(raw.Data))

	for _, m := range raw.Data {
		input, err := strconv.ParseFloat(m.Pricing.Prompt, 64)
		if err != nil || input <= 0 {
			slog.Debug("openrouter: skipping model with missing or zero input price", "id", m.ID)
			continue
		}
		output, err := strconv.ParseFloat(m.Pricing.Completion, 64)
		if err != nil || output <= 0 {
			slog.Debug("openrouter: skipping model with missing or zero output price", "id", m.ID)
			continue
		}

		var ctxWindow *int
		if m.ContextLength > 0 {
			v := m.ContextLength
			ctxWindow = &v
		}

		models = append(models, scraper.ScrapedModel{
			Slug:               m.ID,
			Provider:           providerFromSlug(m.ID),
			InputCostPerToken:  input,
			OutputCostPerToken: output,
			ContextWindow:      ctxWindow,
			Modality:           m.Architecture.Modality,
			SourceName:         "openrouter",
			FetchedAt:          fetchedAt,
		})
	}

	return models, nil
}

// providerFromSlug extracts the provider segment from a "provider/model" slug.
// Returns an empty string if the slug contains no "/" separator.
func providerFromSlug(slug string) string {
	if idx := strings.IndexByte(slug, '/'); idx >= 0 {
		return slug[:idx]
	}
	return ""
}
