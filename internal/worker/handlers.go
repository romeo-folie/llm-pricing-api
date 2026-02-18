package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/scraper"
	"llm-pricing-api/internal/scraper/litellm"
	"llm-pricing-api/internal/scraper/openrouter"
	"llm-pricing-api/internal/scraper/providers"
)

// Handlers holds the shared dependencies for all asynq task handler functions.
// Each handler instantiates its own scraper on every invocation so there is no
// shared HTTP state between runs.
type Handlers struct {
	store      WorkerStore
	reconciler *reconciler.Reconciler
}

// NewHandlers returns a Handlers wired with the given store and reconciler.
func NewHandlers(store WorkerStore, rec *reconciler.Reconciler) *Handlers {
	return &Handlers{store: store, reconciler: rec}
}

// runPipeline executes the full scrape→diff→reconcile pipeline for one source.
// taskName is used for error wrapping and log context; sourceName is the sources
// table name used to pre-fetch matching stored prices for the diff engine.
func (h *Handlers) runPipeline(ctx context.Context, taskName, sourceName string, s scraper.Scraper) error {
	slog.Info("handler: starting", "task", taskName)

	scraped, err := s.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("%s: fetch: %w", taskName, err)
	}

	storedModels, err := h.store.FetchModels(ctx)
	if err != nil {
		return fmt.Errorf("%s: fetch models: %w", taskName, err)
	}

	storedPrices, err := h.store.FetchPricesBySource(ctx, sourceName)
	if err != nil {
		return fmt.Errorf("%s: fetch prices: %w", taskName, err)
	}

	diffs := diff.Diff(storedPrices, storedModels, scraped)
	if err := h.reconciler.Reconcile(ctx, diffs); err != nil {
		return fmt.Errorf("%s: reconcile: %w", taskName, err)
	}

	slog.Info("handler: done", "task", taskName, "model_count", len(scraped))
	return nil
}

// HandleOpenRouterScrape runs the OpenRouter scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleOpenRouterScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskOpenRouterScrape, "openrouter", openrouter.New(nil))
}

// HandleLiteLLMScrape runs the LiteLLM scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleLiteLLMScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskLiteLLMScrape, "litellm", litellm.New(nil))
}

// HandleOpenAIScrape runs the OpenAI HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleOpenAIScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskOpenAIScrape, "openai-docs", providers.NewOpenAI(nil))
}

// HandleAnthropicScrape runs the Anthropic HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleAnthropicScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskAnthropicScrape, "anthropic-docs", providers.NewAnthropic(nil))
}

// HandleGoogleScrape runs the Google HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleGoogleScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskGoogleScrape, "google-docs", providers.NewGoogle(nil))
}

// HandleMistralScrape runs the Mistral HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleMistralScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskMistralScrape, "mistral-docs", providers.NewMistral(nil))
}

// HandleAmazonScrape runs the Amazon Bedrock HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleAmazonScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskAmazonScrape, "amazon-docs", providers.NewAmazon(nil))
}
