package worker

import (
	"context"
	"errors"
	"testing"

	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/scraper"
)

// --- mock WorkerStore ---

type mockStore struct {
	models      []models.Model
	prices      []models.Price
	modelsErr   error
	pricesErr   error
	pricesSource string // last sourceName passed to FetchPricesBySource
}

func (m *mockStore) FetchModels(_ context.Context) ([]models.Model, error) {
	return m.models, m.modelsErr
}

func (m *mockStore) FetchPricesBySource(_ context.Context, sourceName string) ([]models.Price, error) {
	m.pricesSource = sourceName
	return m.prices, m.pricesErr
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
func (s *mockReconcilerStore) LookupModelID(_ context.Context, _ string) (int, error) {
	return 1, nil
}
func (s *mockReconcilerStore) LookupCurrentPrice(_ context.Context, _, _ int) (float64, float64, bool, error) {
	return 0, 0, false, nil
}
func (s *mockReconcilerStore) PublishPrice(_ context.Context, _, _ int, _, _ float64, _ models.Confidence) error {
	return nil
}
func (s *mockReconcilerStore) FlagDiscrepancy(_ context.Context, _, _, _ int, _ models.PriceField, _, _ float64, _ float64) error {
	return nil
}

// newTestHandlers builds a Handlers backed by mock dependencies.
func newTestHandlers(store WorkerStore) *Handlers {
	rec := reconciler.NewWithStore(&mockReconcilerStore{})
	return NewHandlers(store, rec)
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
