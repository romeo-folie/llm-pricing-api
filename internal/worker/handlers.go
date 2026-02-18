package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/reconciler"
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

// HandleOpenRouterScrape runs the OpenRouter scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleOpenRouterScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskOpenRouterScrape
	const sourceName = "openrouter"

	slog.Info("handler: starting", "task", taskName)

	s := openrouter.New(nil)
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

// HandleLiteLLMScrape runs the LiteLLM scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleLiteLLMScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskLiteLLMScrape
	const sourceName = "litellm"

	slog.Info("handler: starting", "task", taskName)

	s := litellm.New(nil)
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

// HandleOpenAIScrape runs the OpenAI HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleOpenAIScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskOpenAIScrape
	const sourceName = "openai-docs"

	slog.Info("handler: starting", "task", taskName)

	s := providers.NewOpenAI(nil)
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

// HandleAnthropicScrape runs the Anthropic HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleAnthropicScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskAnthropicScrape
	const sourceName = "anthropic-docs"

	slog.Info("handler: starting", "task", taskName)

	s := providers.NewAnthropic(nil)
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

// HandleGoogleScrape runs the Google HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleGoogleScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskGoogleScrape
	const sourceName = "google-docs"

	slog.Info("handler: starting", "task", taskName)

	s := providers.NewGoogle(nil)
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

// HandleMistralScrape runs the Mistral HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleMistralScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskMistralScrape
	const sourceName = "mistral-docs"

	slog.Info("handler: starting", "task", taskName)

	s := providers.NewMistral(nil)
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

// HandleAmazonScrape runs the Amazon Bedrock HTML scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleAmazonScrape(ctx context.Context, t *asynq.Task) error {
	const taskName = TaskAmazonScrape
	const sourceName = "amazon-docs"

	slog.Info("handler: starting", "task", taskName)

	s := providers.NewAmazon(nil)
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
