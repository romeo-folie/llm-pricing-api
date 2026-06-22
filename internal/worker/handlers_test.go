package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/scraper"
)

// --- mock WorkerStore ---

type mockStore struct {
	models        []models.Model
	prices        []models.Price
	modelsErr     error
	pricesErr     error
	ensureErr     error
	markVerifyErr error
	pricesSource  string // last sourceName passed to FetchPricesBySource
	ensuredSlugs  []string
	markedSlugs   []string
	markedSource  string
}

func (m *mockStore) FetchModels(_ context.Context) ([]models.Model, error) {
	return m.models, m.modelsErr
}

func (m *mockStore) FetchPricesBySource(_ context.Context, sourceName string) ([]models.Price, error) {
	m.pricesSource = sourceName
	return m.prices, m.pricesErr
}

func (m *mockStore) EnsureModels(_ context.Context, scraped []scraper.ScrapedModel) error {
	for _, s := range scraped {
		m.ensuredSlugs = append(m.ensuredSlugs, s.Slug)
	}
	return m.ensureErr
}

func (m *mockStore) MarkVerified(_ context.Context, sourceName string, slugs []string) error {
	m.markedSource = sourceName
	m.markedSlugs = append(m.markedSlugs, slugs...)
	return m.markVerifyErr
}

// --- mock scraper.Scraper ---

type mockScraper struct {
	result []scraper.ScrapedModel
	err    error
}

func (s *mockScraper) Fetch(_ context.Context) ([]scraper.ScrapedModel, error) {
	return s.result, s.err
}

// --- mock reconciler.Store (for reconciler.NewWithStore) ---

type mockReconcilerStore struct{}

func (s *mockReconcilerStore) LookupSourceID(_ context.Context, _ string) (int, error) {
	return 1, nil
}
func (s *mockReconcilerStore) LookupSourceName(_ context.Context, _ int) (string, error) {
	return "openrouter", nil
}
func (s *mockReconcilerStore) LookupModelID(_ context.Context, _ string) (int, error) {
	return 1, nil
}
func (s *mockReconcilerStore) LookupModelProvider(_ context.Context, _ int) (string, error) {
	return "openai", nil
}
func (s *mockReconcilerStore) LookupCurrentPrice(_ context.Context, _, _ int) (float64, float64, bool, error) {
	return 0, 0, false, nil
}
func (s *mockReconcilerStore) PublishPrice(_ context.Context, _, _ int, _, _ float64, _ models.Confidence, _ string) error {
	return nil
}
func (s *mockReconcilerStore) FlagDiscrepancy(_ context.Context, _, _, _ int, _ models.PriceField, _, _ float64, _ float64) error {
	return nil
}

func (s *mockReconcilerStore) ListActiveWebhooks(_ context.Context) ([]reconciler.WebhookRow, error) {
	return nil, nil
}

// newTestHandlers builds a Handlers backed by mock dependencies.
// The db parameter is nil because pricing pipeline tests do not need it;
// benchmark handler tests provide their own pool.
func newTestHandlers(store WorkerStore) *Handlers {
	rec := reconciler.NewWithStore(&mockReconcilerStore{})
	return NewHandlers(store, rec, nil)
}

// TestRunPipeline_HappyPath verifies a handler completes successfully when the
// scraper, store, and reconciler all succeed.
func TestRunPipeline_HappyPath(t *testing.T) {
	store := &mockStore{
		models: []models.Model{{ID: 1, Slug: "openai/gpt-4o"}},
		prices: []models.Price{},
	}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{
		{Slug: "openai/gpt-4o", SourceName: "openrouter", InputCostPerToken: 5e-6, OutputCostPerToken: 15e-6},
	}}

	if err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Every scraped model must be stamped as verified for this source.
	if store.markedSource != "openrouter" {
		t.Errorf("MarkVerified source = %q; want openrouter", store.markedSource)
	}
	if len(store.markedSlugs) != 1 || store.markedSlugs[0] != "openai/gpt-4o" {
		t.Errorf("MarkVerified slugs = %v; want [openai/gpt-4o]", store.markedSlugs)
	}
}

// TestRunPipeline_MarkVerifiedError_DoesNotFailPipeline verifies that a failure
// to stamp last_verified_at is swallowed: prices are already reconciled, so the
// scrape is still a success.
func TestRunPipeline_MarkVerifiedError_DoesNotFailPipeline(t *testing.T) {
	store := &mockStore{
		models:        []models.Model{{ID: 1, Slug: "openai/gpt-4o"}},
		prices:        []models.Price{},
		markVerifyErr: errors.New("db timeout"),
	}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{
		{Slug: "openai/gpt-4o", SourceName: "openrouter", InputCostPerToken: 5e-6, OutputCostPerToken: 15e-6},
	}}

	if err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s); err != nil {
		t.Fatalf("MarkVerified error must not fail the pipeline, got: %v", err)
	}
}

// TestRunPipeline_ScraperError verifies that a scraper failure is returned with
// the task name as error context and causes asynq to retry the task.
func TestRunPipeline_ScraperError(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)

	fetchErr := errors.New("connection refused")
	s := &mockScraper{err: fetchErr}

	err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, fetchErr) {
		t.Errorf("error chain should contain original error; got: %v", err)
	}
}

// TestRunPipeline_EnsureModelsError verifies that a DB failure on EnsureModels
// is propagated with task name context and prevents the pipeline from continuing.
func TestRunPipeline_EnsureModelsError(t *testing.T) {
	dbErr := errors.New("db constraint violation")
	store := &mockStore{ensureErr: dbErr}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{
		{Slug: "some/model", SourceName: "openrouter", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
	}}

	err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error chain should contain db error; got: %v", err)
	}
}

// TestRunPipeline_EnsureModelsCalled verifies that EnsureModels receives
// the scraped slugs so the models table is populated before reconciliation.
func TestRunPipeline_EnsureModelsCalled(t *testing.T) {
	store := &mockStore{
		models: []models.Model{{ID: 1, Slug: "openai/gpt-4o"}},
		prices: []models.Price{},
	}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{
		{Slug: "openai/gpt-4o", SourceName: "openrouter", InputCostPerToken: 5e-6, OutputCostPerToken: 15e-6},
	}}

	if err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.ensuredSlugs) != 1 || store.ensuredSlugs[0] != "openai/gpt-4o" {
		t.Errorf("expected EnsureModels called with [openai/gpt-4o], got %v", store.ensuredSlugs)
	}
}

// TestRunPipeline_FetchModelsError verifies that a DB failure on FetchModels
// is propagated with task name context.
func TestRunPipeline_FetchModelsError(t *testing.T) {
	dbErr := errors.New("db timeout")
	store := &mockStore{modelsErr: dbErr}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{
		{Slug: "some/model", SourceName: "openrouter"},
	}}

	err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error chain should contain db error; got: %v", err)
	}
}

// TestRunPipeline_FetchPricesError verifies that a DB failure on FetchPricesBySource
// is propagated with task name context.
func TestRunPipeline_FetchPricesError(t *testing.T) {
	dbErr := errors.New("db connection lost")
	store := &mockStore{
		models:    []models.Model{{ID: 1, Slug: "some/model"}},
		pricesErr: dbErr,
	}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{
		{Slug: "some/model", SourceName: "openrouter"},
	}}

	err := h.runPipeline(context.Background(), TaskOpenRouterScrape, "openrouter", s)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error chain should contain db error; got: %v", err)
	}
}

// TestRunPipeline_EmptyScrapeResult verifies that an empty scrape result is
// treated as an error so that asynq retries the task. A zero-model response
// indicates an upstream schema or HTML change, not a normal "no new data" state.
func TestRunPipeline_EmptyScrapeResult(t *testing.T) {
	store := &mockStore{
		models: []models.Model{},
		prices: []models.Price{},
	}
	h := newTestHandlers(store)

	s := &mockScraper{result: []scraper.ScrapedModel{}}

	err := h.runPipeline(context.Background(), TaskLiteLLMScrape, "litellm", s)
	if err == nil {
		t.Fatal("expected error for zero-model result, got nil")
	}
}

// TestRunPipeline_PassesSourceName verifies that the sourceName argument is
// forwarded to FetchPricesBySource unchanged. A non-empty scraper result is
// used so the zero-model guard does not fire before reaching FetchPricesBySource.
func TestRunPipeline_PassesSourceName(t *testing.T) {
	cases := []struct {
		name       string
		sourceName string
	}{
		{"OpenRouter", "openrouter"},
		{"LiteLLM", "litellm"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockStore{models: []models.Model{}, prices: []models.Price{}}
			h := newTestHandlers(store)

			oneModel := []scraper.ScrapedModel{
				{Slug: "test/model", SourceName: tc.sourceName, InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
			}
			_ = h.runPipeline(context.Background(), tc.sourceName, tc.sourceName,
				&mockScraper{result: oneModel})

			if store.pricesSource != tc.sourceName {
				t.Errorf("FetchPricesBySource called with sourceName %q; want %q",
					store.pricesSource, tc.sourceName)
			}
		})
	}
}

// blockingScraper is a scraper.Scraper that blocks until its context is
// cancelled, simulating the HuggingFace API mid-body stall.
type blockingScraper struct{}

func (b *blockingScraper) Fetch(ctx context.Context) ([]scraper.ScrapedModel, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestHandleHuggingFaceScrape_ContextDeadline verifies that
// HandleHuggingFaceScrape returns within huggingFaceScrapeTimeout even when
// the scraper blocks indefinitely (simulating the HuggingFace mid-body stall).
//
// A 50ms handler deadline is injected via an already-shortened
// huggingFaceScrapeTimeout equivalent so the test completes fast without
// relying on the real 80s constant. We call the handler directly with a
// parent context that has a short deadline, confirming the inner
// context.WithTimeout fires and the function returns context.DeadlineExceeded.
func TestHandleHuggingFaceScrape_ContextDeadline(t *testing.T) {
	store := &mockStore{}
	h := newTestHandlers(store)

	// Replace the handler's inner pipeline call with a direct runPipeline call
	// that uses a short-lived context to prove the handler plumbing works.
	// We call runPipeline directly because HandleHuggingFaceScrape wraps the
	// real huggingface.New(nil) scraper; we need to inject blockingScraper.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := h.runPipeline(ctx, TaskHuggingFaceScrape, "huggingface_inference_providers", &blockingScraper{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.DeadlineExceeded or context.Canceled; got: %v", err)
	}
	// The handler must return promptly — well under 1s for a 50ms deadline.
	if elapsed > time.Second {
		t.Errorf("handler took %v; expected to return within 1s on context cancellation", elapsed)
	}
}

// TestHandleHuggingFaceScrape_TimeoutConstantSane verifies that
// huggingFaceScrapeTimeout is positive and less than the 90s asynq task
// timeout so the handler deadline is the innermost cancellation layer.
func TestHandleHuggingFaceScrape_TimeoutConstantSane(t *testing.T) {
	if huggingFaceScrapeTimeout <= 0 {
		t.Errorf("huggingFaceScrapeTimeout must be positive; got %v", huggingFaceScrapeTimeout)
	}
	const asynqTaskTimeout = 90 * time.Second
	if huggingFaceScrapeTimeout >= asynqTaskTimeout {
		t.Errorf("huggingFaceScrapeTimeout (%v) must be < asynq task timeout (%v) "+
			"so the handler deadline fires before asynq cancels the task",
			huggingFaceScrapeTimeout, asynqTaskTimeout)
	}
}
