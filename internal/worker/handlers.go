package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/intelligence"
	"llm-pricing-api/internal/metrics"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/scraper"
	"llm-pricing-api/internal/scraper/anthropic"
	"llm-pricing-api/internal/scraper/bfcl"
	"llm-pricing-api/internal/scraper/chatbot_arena"
	"llm-pricing-api/internal/scraper/gemini"
	"llm-pricing-api/internal/scraper/huggingface"
	"llm-pricing-api/internal/scraper/huggingface_llm"
	"llm-pricing-api/internal/scraper/litellm"
	"llm-pricing-api/internal/scraper/openai"
	"llm-pricing-api/internal/scraper/openrouter"
)

// huggingFaceScrapeTimeout is the maximum wall-clock time allowed for a single
// HuggingFace scrape. The HuggingFace API returns up to 500 large model objects
// and has been observed stalling mid-body for minutes at a time.
//
// Timeout layering (innermost → outermost):
//   - 80s: this handler deadline (context.WithTimeout in HandleHuggingFaceScrape)
//   - 90s: http.Client.Timeout in huggingface.New() — fires after handler deadline
//   - 90s: asynq.Timeout option on the task registration
//
// The http.Client.Timeout is intentionally set to 90s (above this deadline) so that
// the handler context cancellation fires first and the error surfaces as
// context.DeadlineExceeded in logs, rather than the less specific http.Client timeout.
const huggingFaceScrapeTimeout = 80 * time.Second

// Handlers holds the shared dependencies for all asynq task handler functions.
// Each handler instantiates its own scraper on every invocation so there is no
// shared HTTP state between runs.
type Handlers struct {
	store                 WorkerStore
	reconciler            *reconciler.Reconciler
	db                    *pgxpool.Pool // direct pool for benchmark scrapers & intelligence queries
	recomputeCapabilities func(context.Context) error
	logger                zerolog.Logger
}

// NewHandlers returns a Handlers wired with the given store, reconciler, and
// DB pool. The db pool is used by benchmark scraper handlers and intelligence
// recomputation tasks that call the intelligence package directly.
// Real benchmark scrapes synchronously recompute capability scores before the
// task succeeds; the daily recomputation remains a safety net.
// The logger defaults to zerolog.Nop(); call SetLogger to configure one.
func NewHandlers(store WorkerStore, rec *reconciler.Reconciler, db *pgxpool.Pool) *Handlers {
	return &Handlers{
		store:      store,
		reconciler: rec,
		db:         db,
		recomputeCapabilities: func(ctx context.Context) error {
			return intelligence.ComputeAllCapabilityScores(ctx, db)
		},
		logger: zerolog.Nop(),
	}
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

	// Stamp last_verified_at for every model this source reported, whether or not
	// its price changed. This is the freshness signal the API reads: a stable
	// price re-seen on schedule is "verified current now", even though the
	// reconciler (which only fires on a change) left confirmed_at untouched.
	// Best-effort: a stamp failure must not fail an otherwise-successful scrape,
	// since prices have already been reconciled.
	slugs := make([]string, len(scraped))
	for i, m := range scraped {
		slugs[i] = m.Slug
	}
	if err := h.store.MarkVerified(ctx, sourceName, slugs); err != nil {
		h.logger.Warn().Str("task", taskName).Err(err).Msg("handler: mark verified failed")
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
//
// The HuggingFace API has been observed stalling mid-body (after headers arrive
// but before the full JSON payload is delivered), which bypasses http.Client.Timeout
// in some Go runtimes. An explicit context deadline ensures the handler always
// returns within huggingFaceScrapeTimeout regardless of network behaviour.
// The asynq task registered with asynq.Timeout(90s) provides a second safety net.
func (h *Handlers) HandleHuggingFaceScrape(ctx context.Context, _ *asynq.Task) error {
	ctx, cancel := context.WithTimeout(ctx, huggingFaceScrapeTimeout)
	defer cancel()
	return h.runPipeline(ctx, TaskHuggingFaceScrape, "huggingface_inference_providers", huggingface.New(nil))
}

// HandleOpenAIScrape runs the OpenAI HTML pricing scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleOpenAIScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskOpenAIScrape, "openai", openai.New(nil))
}

// HandleAnthropicScrape runs the Anthropic HTML pricing scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleAnthropicScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskAnthropicScrape, "anthropic", anthropic.New(nil))
}

// HandleGeminiScrape runs the Google Gemini HTML pricing scraper and feeds the result
// through the diff and reconciliation pipeline.
func (h *Handlers) HandleGeminiScrape(ctx context.Context, _ *asynq.Task) error {
	return h.runPipeline(ctx, TaskGeminiScrape, "google", gemini.New(nil))
}

// ---------------------------------------------------------------------------
// Benchmark scraper handlers
// ---------------------------------------------------------------------------

func (h *Handlers) runBenchmarkScrape(ctx context.Context, taskName string, s scraper.BenchmarkScraper) error {
	if err := s.Scrape(ctx); err != nil {
		return fmt.Errorf("%s: %w", taskName, err)
	}
	if err := h.recomputeCapabilities(ctx); err != nil {
		return fmt.Errorf("%s: recompute capabilities: %w", taskName, err)
	}
	return nil
}

// HandleBFCLScrape runs the SWE-bench Verified leaderboard scraper. The legacy
// task/package name is retained to avoid breaking already queued jobs.
func (h *Handlers) HandleBFCLScrape(ctx context.Context, _ *asynq.Task) error {
	h.logger.Info().Msg("handler: starting SWE-bench scrape")
	s := bfcl.New(h.db, nil)
	s.SetLogger(h.logger)
	if err := h.runBenchmarkScrape(ctx, TaskBFCLScrape, s); err != nil {
		return err
	}
	h.logger.Info().Msg("handler: SWE-bench scrape and capability recompute complete")
	return nil
}

// HandleHuggingFaceLLMScrape runs the LiveCodeBench scraper. The legacy
// task/package name is retained to avoid breaking already queued jobs.
func (h *Handlers) HandleHuggingFaceLLMScrape(ctx context.Context, _ *asynq.Task) error {
	h.logger.Info().Msg("handler: starting LiveCodeBench scrape")
	s := huggingface_llm.New(h.db, nil)
	s.SetLogger(h.logger)
	if err := h.runBenchmarkScrape(ctx, TaskHuggingFaceLLMScrape, s); err != nil {
		return err
	}
	h.logger.Info().Msg("handler: LiveCodeBench scrape and capability recompute complete")
	return nil
}

// HandleChatbotArenaScrape retains compatibility with already queued tasks.
// Arena has no working public source, so it explicitly skips without claiming
// that evidence or capability scores were updated.
func (h *Handlers) HandleChatbotArenaScrape(ctx context.Context, _ *asynq.Task) error {
	s := chatbot_arena.New(h.db, nil)
	s.SetLogger(h.logger)
	if err := s.Scrape(ctx); err != nil {
		return fmt.Errorf("benchmark:chatbot_arena: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Intelligence recomputation handlers
// ---------------------------------------------------------------------------

// HandleRecomputeCapabilityScores recomputes capability scores for all models
// that have benchmark data. This is run daily and after each benchmark scrape.
func (h *Handlers) HandleRecomputeCapabilityScores(ctx context.Context, _ *asynq.Task) error {
	h.logger.Info().Msg("handler: recomputing all capability scores")
	if err := h.recomputeCapabilities(ctx); err != nil {
		return fmt.Errorf("intelligence:recompute: %w", err)
	}
	h.logger.Info().Msg("handler: capability scores recomputed")
	return nil
}

// HandleStalenessCheck is a compatibility alias for old queued tasks. Freshness
// is calculated per dimension by the exact recomputation path; the former
// model-wide SQL incorrectly let stale evidence in one dimension taint others.
func (h *Handlers) HandleStalenessCheck(ctx context.Context, _ *asynq.Task) error {
	return h.HandleRecomputeCapabilityScores(ctx, nil)
}
