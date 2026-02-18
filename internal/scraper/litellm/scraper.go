package litellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"llm-pricing-api/internal/scraper"
)

const defaultURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// Scraper fetches model pricing from the LiteLLM model cost map on GitHub daily.
type Scraper struct {
	client *http.Client
	url    string
}

// New returns a Scraper using the provided HTTP client.
// If client is nil a default client with a 15s timeout is used.
func New(client *http.Client) *Scraper {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Scraper{client: client, url: defaultURL}
}

// liteLLMEntry represents one model entry in the LiteLLM cost map JSON.
type liteLLMEntry struct {
	InputCostPerToken  *float64 `json:"input_cost_per_token"`
	OutputCostPerToken *float64 `json:"output_cost_per_token"`
	// MaxInputTokens is the input context window. Note: max_tokens is a legacy LiteLLM field
	// that mirrors the *output* cap and must not be used for context window.
	MaxInputTokens  *int   `json:"max_input_tokens"`
	LiteLLMProvider string `json:"litellm_provider"`
	// Mode maps to the LiteLLM "mode" field (e.g. "chat", "embedding", "image_generation").
	Mode string `json:"mode"`
}

// modalityFromMode converts a LiteLLM mode string to a ScrapedModel Modality value.
func modalityFromMode(mode string) string {
	switch mode {
	case "embedding":
		return "embedding"
	case "image_generation":
		return "image"
	case "audio_transcription", "audio_speech":
		return "audio"
	case "multimodal":
		return "multimodal"
	default:
		// "chat", "text_completion", "moderations", "", or any unknown value → text.
		return "text"
	}
}

// Fetch retrieves all models from the LiteLLM GitHub cost map and normalises them.
// Models with missing or zero prices are skipped: absent prices indicate incomplete entries,
// and zero prices indicate free/locally-hosted models that are outside the scope of this
// platform (same rationale as the OpenRouter scraper). Model IDs are used as-is for slugs.
func (s *Scraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("litellm: build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("litellm: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		_, _ = io.Copy(io.Discard, resp.Body)
		slog.Warn("litellm: unexpected status", "status", resp.StatusCode, "body_excerpt", string(buf[:n]))
		return nil, fmt.Errorf("litellm: unexpected status %d", resp.StatusCode)
	}

	var raw map[string]liteLLMEntry
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("litellm: decode: %w", err)
	}

	fetchedAt := time.Now().UTC()
	models := make([]scraper.ScrapedModel, 0, len(raw))

	for key, entry := range raw {
		if entry.InputCostPerToken == nil || entry.OutputCostPerToken == nil {
			slog.Debug("litellm: skipping model with missing prices", "key", key)
			continue
		}
		if *entry.InputCostPerToken <= 0 || *entry.OutputCostPerToken <= 0 {
			slog.Debug("litellm: skipping model with zero or negative price", "key", key)
			continue
		}

		provider := entry.LiteLLMProvider
		if provider == "" {
			// Fall back to the prefix before "/" in the key (e.g. "gemini/gemini-pro" → "gemini").
			if idx := strings.IndexByte(key, '/'); idx >= 0 {
				provider = key[:idx]
			}
		}

		// Deep-copy the *int pointer so that each ScrapedModel owns an
		// independent integer — sharing the decoded pointer would allow
		// a future in-place mutation of one model's ContextWindow to
		// silently corrupt the JSON-decoded map entry.
		var ctxWindow *int
		if entry.MaxInputTokens != nil {
			v := *entry.MaxInputTokens
			ctxWindow = &v
		}

		models = append(models, scraper.ScrapedModel{
			Slug:               key,
			Provider:           provider,
			InputCostPerToken:  *entry.InputCostPerToken,
			OutputCostPerToken: *entry.OutputCostPerToken,
			ContextWindow:      ctxWindow,
			Modality:           modalityFromMode(entry.Mode),
			SourceName:         "litellm",
			FetchedAt:          fetchedAt,
		})
	}

	return models, nil
}
