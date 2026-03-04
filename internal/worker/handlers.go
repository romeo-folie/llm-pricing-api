package worker

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/metrics"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/scraper"
	"llm-pricing-api/internal/scraper/huggingface"
	"llm-pricing-api/internal/scraper/litellm"
	"llm-pricing-api/internal/scraper/openrouter"
)

// Handlers holds the shared dependencies for all asynq task handler functions.
// Each handler instantiates its own scraper on every invocation so there is no
// shared HTTP state between runs.
type Handlers struct {
	store      WorkerStore
	reconciler *reconciler.Reconciler
	logger     zerolog.Logger
}

// NewHandlers returns a Handlers wired with the given store and reconciler.
// The logger defaults to zerolog.Nop(); call SetLogger to configure one.
func NewHandlers(store WorkerStore, rec *reconciler.Reconciler) *Handlers {
	return &Handlers{store: store, reconciler: rec, logger: zerolog.Nop()}
}

// SetLogger configures the zerolog.Logger used by the Handlers.
func (h *Handlers) SetLogger(l zerolog.Logger) {
	h.logger = l
}

// runPipeline executes the full scrape→diff→reconcile pipeline for one source.
// taskName is used for error wrapping and log context; sourceName is the sources
// table name used to pre-fetch matching stored prices for the diff engine.
func (h *Handlers) runPipeline(ctx context.Context, taskName, sourceName string, s scraper.Scraper) error {
	h.logger.Info().Str("task", taskName).Msg("handler: starting")
	status := "error"
	defer func() {
		metrics.ScraperRunsTotal.WithLabelValues(sourceName, status).Inc()
	}()

	scraped, err := s.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("%s: fetch: %w", taskName, err)
	}
	if len(scraped) == 0 {
		// An empty result after a successful fetch indicates an upstream schema
		// change or an HTML structure change that broke parsing. Treat this as
		// an error so asynq retries the task and the failure appears in logs —
		// silently skipping the diff+reconcile pipeline would stall price updates.
		return fmt.Errorf("%s: scraper returned 0 models; possible upstream change", taskName)
	}

	// Upsert model rows so the reconciler's LookupModelID succeeds for
	// newly discovered models. On a fresh database this populates the
	// entire models table from the first scrape.
	h.logger.Info().Str("task", taskName).Int("scraped_count", len(scraped)).Msg("handler: ensuring models")
	if err := h.store.EnsureModels(ctx, scraped); err != nil {
		return fmt.Errorf("%s: ensure models: %w", taskName, err)
	}
	h.logger.Info().Str("task", taskName).Msg("handler: models ensured")

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

	h.logger.Info().Str("task", taskName).Int("model_count", len(scraped)).Msg("handler: done")
	status = "success"
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

// HandleHuggingFaceScrape runs the HuggingFace Inference Providers scraper and feeds
// the result through the diff and reconciliation pipeline.
func (h *Handlers) HandleHuggingFaceScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskHuggingFaceScrape, "huggingface_inference_providers", huggingface.New(nil))
}
