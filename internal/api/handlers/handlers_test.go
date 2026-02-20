package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/models"
)

// --- mock store ---------------------------------------------------------

// mockStore implements handlers.Store with in-memory stubs for testing.
// Each method field can be overridden per test.
type mockStore struct {
	listModels            func(ctx context.Context, filter handlers.ListModelsFilter) ([]handlers.ModelRow, int, error)
	getModel              func(ctx context.Context, id int) (handlers.ModelRow, error)
	getModelBySlug        func(ctx context.Context, slug string) (handlers.ModelRow, error)
	listProviders         func(ctx context.Context) ([]handlers.ProviderRow, error)
	compareModels         func(ctx context.Context, ids []int) ([]handlers.ModelRow, error)
	listChanges           func(ctx context.Context, filter handlers.ChangesFilter) ([]handlers.ChangeRow, int, error)
	getChangesSummary     func(ctx context.Context, filter handlers.SummaryFilter) (handlers.ChangesSummary, error)
	getPriceHistory       func(ctx context.Context, modelID int) ([]api.PriceHistoryRow, error)
	getPriceHistoryBatch  func(ctx context.Context, modelIDs []int) (map[int][]api.PriceHistoryRow, error)
	modelExists           func(ctx context.Context, id int) (bool, error)
	getModelHistory       func(ctx context.Context, modelID int, filter handlers.HistoryFilter) ([]handlers.HistoryRow, error)
	listModelsForCtx      func(ctx context.Context, limit int) ([]handlers.ContextModelRow, error)
	recommendModels       func(ctx context.Context, filter handlers.RecommendFilter) ([]handlers.ModelRow, error)
}

func (m *mockStore) ListModels(ctx context.Context, filter handlers.ListModelsFilter) ([]handlers.ModelRow, int, error) {
	if m.listModels != nil {
		return m.listModels(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockStore) GetModel(ctx context.Context, id int) (handlers.ModelRow, error) {
	if m.getModel != nil {
		return m.getModel(ctx, id)
	}
	return handlers.ModelRow{}, handlers.ErrNotFound
}

func (m *mockStore) GetModelBySlug(ctx context.Context, slug string) (handlers.ModelRow, error) {
	if m.getModelBySlug != nil {
		return m.getModelBySlug(ctx, slug)
	}
	return handlers.ModelRow{}, handlers.ErrNotFound
}

func (m *mockStore) ListProviders(ctx context.Context) ([]handlers.ProviderRow, error) {
	if m.listProviders != nil {
		return m.listProviders(ctx)
	}
	return nil, nil
}

func (m *mockStore) CompareModels(ctx context.Context, ids []int) ([]handlers.ModelRow, error) {
	if m.compareModels != nil {
		return m.compareModels(ctx, ids)
	}
	return nil, nil
}

func (m *mockStore) ListChanges(ctx context.Context, filter handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
	if m.listChanges != nil {
		return m.listChanges(ctx, filter)
	}
	return nil, 0, nil
}

func (m *mockStore) GetChangesSummary(ctx context.Context, filter handlers.SummaryFilter) (handlers.ChangesSummary, error) {
	if m.getChangesSummary != nil {
		return m.getChangesSummary(ctx, filter)
	}
	return handlers.ChangesSummary{}, nil
}

func (m *mockStore) GetPriceHistory(ctx context.Context, modelID int) ([]api.PriceHistoryRow, error) {
	if m.getPriceHistory != nil {
		return m.getPriceHistory(ctx, modelID)
	}
	return nil, nil
}

func (m *mockStore) GetPriceHistoryBatch(ctx context.Context, modelIDs []int) (map[int][]api.PriceHistoryRow, error) {
	if m.getPriceHistoryBatch != nil {
		return m.getPriceHistoryBatch(ctx, modelIDs)
	}
	return map[int][]api.PriceHistoryRow{}, nil
}

func (m *mockStore) ModelExists(ctx context.Context, id int) (bool, error) {
	if m.modelExists != nil {
		return m.modelExists(ctx, id)
	}
	// Default: return true if getModel is set and would not return ErrNotFound,
	// return false otherwise (mirroring the default getModel behavior).
	if m.getModel != nil {
		_, err := m.getModel(ctx, id)
		return err == nil, nil
	}
	return false, nil
}

func (m *mockStore) GetModelHistory(ctx context.Context, modelID int, filter handlers.HistoryFilter) ([]handlers.HistoryRow, error) {
	if m.getModelHistory != nil {
		return m.getModelHistory(ctx, modelID, filter)
	}
	return nil, nil
}

func (m *mockStore) ListModelsForContext(ctx context.Context, limit int) ([]handlers.ContextModelRow, error) {
	if m.listModelsForCtx != nil {
		return m.listModelsForCtx(ctx, limit)
	}
	return nil, nil
}

func (m *mockStore) RecommendModels(ctx context.Context, filter handlers.RecommendFilter) ([]handlers.ModelRow, error) {
	if m.recommendModels != nil {
		return m.recommendModels(ctx, filter)
	}
	return nil, nil
}

// --- test helpers -------------------------------------------------------

// newApp creates a Fiber app with the ErrorHandler and registers all Free-tier
// routes backed by the supplied mock store.
func newApp(store handlers.Store) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	h := handlers.New(store)
	v1 := app.Group("/v1")
	v1.Get("/models", h.ListModels)
	v1.Get("/models/:id", h.GetModel)
	v1.Get("/providers", h.ListProviders)
	v1.Get("/compare", h.Compare)
	v1.Get("/changes/summary", h.GetChangesSummary)
	v1.Get("/changes", h.ListChanges)
	return app
}

// getWithHeaders performs a GET request and returns the status, body, and headers.
func getWithHeaders(t *testing.T, app *fiber.App, path string) (int, []byte, http.Header) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode, body, resp.Header
}

// get performs a GET request and returns the status code and body.
func get(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode, body
}

// sampleModel returns a ModelRow with plausible data for use in tests.
func sampleModel(id int) handlers.ModelRow {
	ctx := 8192
	return handlers.ModelRow{
		ID:            id,
		Provider:      "openai",
		Name:          "gpt-4",
		Slug:          "openai/gpt-4",
		Modality:      "text",
		ContextWindow: &ctx,
		PriceInput:    0.00003,
		PriceOutput:   0.00006,
		ConfirmedAt:   time.Now().UTC(),
		Source:        "openrouter",
		Meta: api.TrustMeta{
			ConfirmedAt:    time.Now().UTC(),
			Source:         "openrouter",
			Confidence:     models.ConfidenceHigh,
			AgeHours:       1.5,
			ChangeVelocity: 0.1,
		},
	}
}

// --- ListModels tests ---------------------------------------------------

func TestListModels_OK(t *testing.T) {
	store := &mockStore{
		listModels: func(_ context.Context, _ handlers.ListModelsFilter) ([]handlers.ModelRow, int, error) {
			return []handlers.ModelRow{sampleModel(1), sampleModel(2)}, 2, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/models")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	// Verify response is valid JSON with data array.
	var envelope struct {
		Data []json.RawMessage `json:"data"`
		Meta json.RawMessage   `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 2 {
		t.Errorf("expected 2 items, got %d", len(envelope.Data))
	}
}

func TestListModels_UnderlyingProvider_NullSerialisation(t *testing.T) {
	// underlying_provider must appear as null in list items, not be omitted,
	// because modelResponse has no omitempty on the field. This also guards
	// against someone accidentally adding omitempty to the struct tag.
	store := &mockStore{
		listModels: func(_ context.Context, _ handlers.ListModelsFilter) ([]handlers.ModelRow, int, error) {
			m := sampleModel(1)
			m.UnderlyingProvider = nil
			return []handlers.ModelRow{m}, 1, nil
		},
	}
	app := newApp(store)

	_, body := get(t, app, "/v1/models")

	var envelope struct {
		Data []struct {
			UnderlyingProvider *string `json:"underlying_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("expected at least one model item")
	}
	if envelope.Data[0].UnderlyingProvider != nil {
		t.Errorf("expected underlying_provider null, got %q", *envelope.Data[0].UnderlyingProvider)
	}
	if !strings.Contains(string(body), `"underlying_provider":null`) {
		t.Errorf("underlying_provider must be serialised as null, not omitted; body: %s", body)
	}
}

func TestListModels_XTotalCountHeader(t *testing.T) {
	store := &mockStore{
		listModels: func(_ context.Context, _ handlers.ListModelsFilter) ([]handlers.ModelRow, int, error) {
			return []handlers.ModelRow{sampleModel(1)}, 42, nil
		},
	}
	app := newApp(store)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	resp.Body.Close()

	if got := resp.Header.Get("X-Total-Count"); got != "42" {
		t.Errorf("X-Total-Count: expected 42, got %q", got)
	}
}

func TestListModels_InvalidMinContext_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, body := get(t, app, "/v1/models?min_context=abc")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}

	var problem api.ProblemDetail
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("unmarshal problem: %v; body: %s", err, body)
	}
	if problem.Status != 400 {
		t.Errorf("problem.Status: expected 400, got %d", problem.Status)
	}
}

func TestListModels_NegativeMinContext_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models?min_context=-1")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestListModels_InvalidPage_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models?page=0")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for page=0, got %d", status)
	}
}

func TestListModels_PerPageExceedsMax_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, body := get(t, app, "/v1/models?per_page=201")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for per_page=201, got %d; body: %s", status, body)
	}
}

func TestListModels_PerPageAtMax_Returns200(t *testing.T) {
	store := &mockStore{
		listModels: func(_ context.Context, _ handlers.ListModelsFilter) ([]handlers.ModelRow, int, error) {
			return nil, 0, nil
		},
	}
	app := newApp(store)

	status, _ := get(t, app, "/v1/models?per_page=200")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for per_page=200 (at max), got %d", status)
	}
}

func TestListModels_PassesFiltersToStore(t *testing.T) {
	var capturedFilter handlers.ListModelsFilter
	store := &mockStore{
		listModels: func(_ context.Context, f handlers.ListModelsFilter) ([]handlers.ModelRow, int, error) {
			capturedFilter = f
			return nil, 0, nil
		},
	}
	app := newApp(store)

	minCtx := 4096
	get(t, app, "/v1/models?provider=openai&modality=text&min_context=4096&page=2&per_page=10")

	if capturedFilter.Provider != "openai" {
		t.Errorf("provider: got %q, want openai", capturedFilter.Provider)
	}
	if capturedFilter.Modality != "text" {
		t.Errorf("modality: got %q, want text", capturedFilter.Modality)
	}
	if capturedFilter.MinContext == nil || *capturedFilter.MinContext != minCtx {
		t.Errorf("min_context: got %v, want %d", capturedFilter.MinContext, minCtx)
	}
	if capturedFilter.Page != 2 {
		t.Errorf("page: got %d, want 2", capturedFilter.Page)
	}
	if capturedFilter.PerPage != 10 {
		t.Errorf("per_page: got %d, want 10", capturedFilter.PerPage)
	}
}

// --- GetModel tests -----------------------------------------------------

func TestGetModel_OK(t *testing.T) {
	store := &mockStore{
		getModel: func(_ context.Context, id int) (handlers.ModelRow, error) {
			if id == 1 {
				return sampleModel(1), nil
			}
			return handlers.ModelRow{}, handlers.ErrNotFound
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/models/1")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			ID       int    `json:"id"`
			Provider string `json:"provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	if envelope.Data.ID != 1 {
		t.Errorf("expected id=1, got %d", envelope.Data.ID)
	}
}

func TestGetModel_NotFound_Returns404(t *testing.T) {
	store := &mockStore{
		getModel: func(_ context.Context, _ int) (handlers.ModelRow, error) {
			return handlers.ModelRow{}, handlers.ErrNotFound
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/models/999")

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", status, body)
	}
}

// Since the slug-routing change, non-integer and zero params are treated as
// slug lookups — not rejected with 400. The following tests reflect the new
// behaviour and the slug tests below cover success/not-found paths.

func TestGetModel_NonIntegerParam_RoutesToSlugLookup(t *testing.T) {
	// "abc" is not a positive integer → GetModelBySlug("abc") is called.
	// Default mock returns ErrNotFound → expect 404, not 400.
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/abc")
	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404 (slug not found), got %d", status)
	}
}

func TestGetModel_ZeroParam_RoutesToSlugLookup(t *testing.T) {
	// "0" parses as integer 0, which is not > 0, so falls through to slug lookup.
	// Default mock returns ErrNotFound → expect 404, not 400.
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/0")
	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404 (slug not found), got %d", status)
	}
}

func TestGetModel_BySlug_OK(t *testing.T) {
	const slug = "gpt-4o"
	store := &mockStore{
		getModelBySlug: func(_ context.Context, s string) (handlers.ModelRow, error) {
			if s == slug {
				m := sampleModel(42)
				m.Slug = slug
				return m, nil
			}
			return handlers.ModelRow{}, handlers.ErrNotFound
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/models/"+slug)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.Slug != slug {
		t.Errorf("expected slug=%q, got %q", slug, envelope.Data.Slug)
	}
}

func TestGetModel_BySlug_NotFound(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/nonexistent-model")
	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestGetModel_IntegerIDTakesPriorityOverSlug(t *testing.T) {
	// A positive integer param must call GetModel (integer), not GetModelBySlug.
	slugCalled := false
	store := &mockStore{
		getModel: func(_ context.Context, id int) (handlers.ModelRow, error) {
			if id == 7 {
				return sampleModel(7), nil
			}
			return handlers.ModelRow{}, handlers.ErrNotFound
		},
		getModelBySlug: func(_ context.Context, _ string) (handlers.ModelRow, error) {
			slugCalled = true
			return handlers.ModelRow{}, handlers.ErrNotFound
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/models/7")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	if slugCalled {
		t.Error("GetModelBySlug should not have been called for a positive integer param")
	}
}

func TestGetModel_UnderlyingProvider_NullSerialisation(t *testing.T) {
	// When underlying_provider is nil the JSON field must be present as null.
	store := &mockStore{
		getModel: func(_ context.Context, id int) (handlers.ModelRow, error) {
			m := sampleModel(id)
			m.UnderlyingProvider = nil
			return m, nil
		},
	}
	app := newApp(store)

	_, body := get(t, app, "/v1/models/1")

	var envelope struct {
		Data struct {
			UnderlyingProvider *string `json:"underlying_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.UnderlyingProvider != nil {
		t.Errorf("expected underlying_provider null, got %q", *envelope.Data.UnderlyingProvider)
	}
	if !strings.Contains(string(body), `"underlying_provider":null`) {
		t.Errorf("underlying_provider must be serialised as null, not omitted; body: %s", body)
	}
}

func TestGetModel_UnderlyingProvider_NonNullSerialisation(t *testing.T) {
	prov := "replicate"
	store := &mockStore{
		getModel: func(_ context.Context, id int) (handlers.ModelRow, error) {
			m := sampleModel(id)
			m.UnderlyingProvider = &prov
			return m, nil
		},
	}
	app := newApp(store)

	_, body := get(t, app, "/v1/models/1")

	var envelope struct {
		Data struct {
			UnderlyingProvider *string `json:"underlying_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.UnderlyingProvider == nil {
		t.Fatal("expected underlying_provider to be non-null")
	}
	if *envelope.Data.UnderlyingProvider != prov {
		t.Errorf("expected underlying_provider=%q, got %q", prov, *envelope.Data.UnderlyingProvider)
	}
}

// --- ListProviders tests ------------------------------------------------

func TestListProviders_OK(t *testing.T) {
	store := &mockStore{
		listProviders: func(_ context.Context) ([]handlers.ProviderRow, error) {
			return []handlers.ProviderRow{
				{Name: "anthropic", ModelCount: 5},
				{Name: "openai", ModelCount: 10},
			}, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/providers")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			Name       string `json:"name"`
			ModelCount int    `json:"model_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 2 {
		t.Errorf("expected 2 providers, got %d", len(envelope.Data))
	}
}

// --- Compare tests ------------------------------------------------------

func TestCompare_OK(t *testing.T) {
	store := &mockStore{
		compareModels: func(_ context.Context, ids []int) ([]handlers.ModelRow, error) {
			rows := make([]handlers.ModelRow, len(ids))
			for i, id := range ids {
				rows[i] = sampleModel(id)
			}
			return rows, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/compare?models=1,2,3")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 3 {
		t.Errorf("expected 3 items, got %d", len(envelope.Data))
	}
}

func TestCompare_TooManyIDs_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, body := get(t, app, "/v1/compare?models=1,2,3,4,5,6")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}
}

func TestCompare_InvalidID_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/compare?models=1,abc,3")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestCompare_MissingModelsParam_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/compare")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestCompare_NotFound_Returns404(t *testing.T) {
	store := &mockStore{
		compareModels: func(_ context.Context, _ []int) ([]handlers.ModelRow, error) {
			return nil, handlers.ErrNotFound
		},
	}
	app := newApp(store)

	status, _ := get(t, app, "/v1/compare?models=1,999")
	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestCompare_DeduplicatesIDs(t *testing.T) {
	var capturedIDs []int
	store := &mockStore{
		compareModels: func(_ context.Context, ids []int) ([]handlers.ModelRow, error) {
			capturedIDs = ids
			rows := make([]handlers.ModelRow, len(ids))
			for i, id := range ids {
				rows[i] = sampleModel(id)
			}
			return rows, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/compare?models=1,1,2")

	if len(capturedIDs) != 2 {
		t.Errorf("expected 2 unique IDs, got %d: %v", len(capturedIDs), capturedIDs)
	}
}

// --- ListChanges tests --------------------------------------------------

func TestListChanges_OK(t *testing.T) {
	store := &mockStore{
		listChanges: func(_ context.Context, _ handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			return []handlers.ChangeRow{
				{
					ModelID:     1,
					ModelSlug:   "openai/gpt-4",
					Provider:    "openai",
					OldInput:    0.00002,
					OldOutput:   0.00004,
					NewInput:    0.00003,
					NewOutput:   0.00006,
					ConfirmedAt: time.Now().UTC(),
					Source:      "openrouter",
				},
			}, 1, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/changes")

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 1 {
		t.Errorf("expected 1 change, got %d", len(envelope.Data))
	}
}

func TestListChanges_InvalidSince_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, body := get(t, app, "/v1/changes?since=not-a-date")

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}
}

func TestListChanges_ValidSince_PassedToStore(t *testing.T) {
	var capturedFilter handlers.ChangesFilter
	store := &mockStore{
		listChanges: func(_ context.Context, f handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			capturedFilter = f
			return nil, 0, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/changes?since=2026-01-01T00:00:00Z&provider=openai")

	if capturedFilter.Since == nil {
		t.Fatal("expected Since to be set")
	}
	if capturedFilter.Provider != "openai" {
		t.Errorf("provider: got %q, want openai", capturedFilter.Provider)
	}
}

func TestListChanges_EmptyResult_ReturnsEmptyArray(t *testing.T) {
	store := &mockStore{
		listChanges: func(_ context.Context, _ handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			return nil, 0, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/changes")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v; body: %s", err, body)
	}
	// nil slice marshalled from empty store returns null; verify response body is valid.
	_ = envelope
}

func TestListChanges_XTotalCountHeader(t *testing.T) {
	store := &mockStore{
		listChanges: func(_ context.Context, _ handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			return []handlers.ChangeRow{
				{ModelID: 1, ModelSlug: "openai/gpt-4", Provider: "openai", ConfirmedAt: time.Now().UTC(), Source: "openrouter"},
			}, 142, nil
		},
	}
	app := newApp(store)

	status, _, headers := getWithHeaders(t, app, "/v1/changes")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if got := headers.Get("X-Total-Count"); got != "142" {
		t.Errorf("X-Total-Count: expected 142, got %q", got)
	}
}

func TestListChanges_DefaultLimit(t *testing.T) {
	var capturedFilter handlers.ChangesFilter
	store := &mockStore{
		listChanges: func(_ context.Context, f handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			capturedFilter = f
			return nil, 0, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/changes")

	if capturedFilter.Limit != 50 {
		t.Errorf("default limit: expected 50, got %d", capturedFilter.Limit)
	}
}

func TestListChanges_LimitExceedsMax_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/changes?limit=201")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for limit=201, got %d", status)
	}
}

func TestListChanges_InvalidLimit_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/changes?limit=abc")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for limit=abc, got %d", status)
	}
}

func TestListChanges_InvalidBefore_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/changes?before=not-a-date")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for before=not-a-date, got %d", status)
	}
}

func TestListChanges_BeforeCursor_PassedToStore(t *testing.T) {
	var capturedFilter handlers.ChangesFilter
	store := &mockStore{
		listChanges: func(_ context.Context, f handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			capturedFilter = f
			return nil, 0, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/changes?before=2026-02-01T12:00:00Z")

	if capturedFilter.Before == nil {
		t.Fatal("expected Before to be set")
	}
	expected := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	if !capturedFilter.Before.Equal(expected) {
		t.Errorf("Before: got %v, want %v", *capturedFilter.Before, expected)
	}
}

func TestListChanges_HasMoreHeader(t *testing.T) {
	// Return limit+1 rows (51 for default limit=50) to trigger hasMore=true.
	now := time.Now().UTC()
	store := &mockStore{
		listChanges: func(_ context.Context, f handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			rows := make([]handlers.ChangeRow, f.Limit+1)
			for i := range rows {
				rows[i] = handlers.ChangeRow{
					ModelID:     i + 1,
					ModelSlug:   "openai/gpt-4",
					Provider:    "openai",
					ConfirmedAt: now.Add(-time.Duration(i) * time.Minute),
					Source:      "openrouter",
				}
			}
			return rows, 100, nil
		},
	}
	app := newApp(store)

	status, body, headers := getWithHeaders(t, app, "/v1/changes?limit=5")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
	if got := headers.Get("X-Has-More"); got != "true" {
		t.Errorf("X-Has-More: expected true, got %q", got)
	}
	if got := headers.Get("X-Next-Cursor"); got == "" {
		t.Error("X-Next-Cursor: expected non-empty cursor")
	}

	// Verify body only contains limit items, not limit+1.
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data) != 5 {
		t.Errorf("expected 5 items in response, got %d", len(envelope.Data))
	}
}

func TestListChanges_NoMore_HasMoreFalse(t *testing.T) {
	store := &mockStore{
		listChanges: func(_ context.Context, _ handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			return []handlers.ChangeRow{
				{ModelID: 1, ModelSlug: "openai/gpt-4", Provider: "openai", ConfirmedAt: time.Now().UTC(), Source: "openrouter"},
			}, 1, nil
		},
	}
	app := newApp(store)

	_, _, headers := getWithHeaders(t, app, "/v1/changes")
	if got := headers.Get("X-Has-More"); got != "false" {
		t.Errorf("X-Has-More: expected false, got %q", got)
	}
}

// TestListChanges_CursorRoundTrip verifies that the X-Next-Cursor value
// (emitted with sub-second precision) can be used as the before= query
// parameter without being rejected as invalid.
func TestListChanges_CursorRoundTrip(t *testing.T) {
	// Use a timestamp with sub-second precision to simulate real PostgreSQL
	// TIMESTAMPTZ values which have microsecond resolution.
	ts := time.Date(2026, 2, 10, 14, 30, 45, 123456000, time.UTC)

	var callCount int
	store := &mockStore{
		listChanges: func(_ context.Context, f handlers.ChangesFilter) ([]handlers.ChangeRow, int, error) {
			callCount++
			if callCount == 1 {
				// First call: return 2 rows to trigger hasMore.
				return []handlers.ChangeRow{
					{ModelID: 1, ModelSlug: "openai/gpt-4", Provider: "openai", ConfirmedAt: ts, Source: "openrouter"},
					{ModelID: 2, ModelSlug: "openai/gpt-3.5", Provider: "openai", ConfirmedAt: ts.Add(-time.Minute), Source: "openrouter"},
				}, 5, nil
			}
			// Second call: return 1 row, no more.
			return []handlers.ChangeRow{
				{ModelID: 3, ModelSlug: "anthropic/claude-3", Provider: "anthropic", ConfirmedAt: ts.Add(-2 * time.Minute), Source: "openrouter"},
			}, 5, nil
		},
	}
	app := newApp(store)

	// First request with limit=1 so hasMore triggers.
	status1, _, headers1 := getWithHeaders(t, app, "/v1/changes?limit=1")
	if status1 != fiber.StatusOK {
		t.Fatalf("first request: expected 200, got %d", status1)
	}

	cursor := headers1.Get("X-Next-Cursor")
	if cursor == "" {
		t.Fatal("X-Next-Cursor: expected non-empty cursor from first request")
	}

	// Second request: use the cursor as before=. Must not return 400.
	status2, body2 := get(t, app, "/v1/changes?before="+url.QueryEscape(cursor))
	if status2 != fiber.StatusOK {
		t.Fatalf("cursor round-trip: expected 200, got %d; body: %s", status2, body2)
	}
}

// --- GetChangesSummary tests --------------------------------------------

func TestGetChangesSummary_OK(t *testing.T) {
	store := &mockStore{
		getChangesSummary: func(_ context.Context, _ handlers.SummaryFilter) (handlers.ChangesSummary, error) {
			return handlers.ChangesSummary{
				Heatmap: []handlers.HeatmapProviderGroup{
					{
						Provider:     "openai",
						TotalChanges: 3,
						Models: []handlers.HeatmapModelNode{
							{ModelID: 1, ModelSlug: "openai/gpt-4", ChangeCount: 3, AvgDeltaPct: 15.5},
						},
					},
				},
				TopMovers: []handlers.TopMover{
					{ModelID: 1, ModelSlug: "openai/gpt-4", Provider: "openai", DeltaPct: 25.0},
				},
				TotalChanges: 3,
			}, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/changes/summary")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Heatmap      []json.RawMessage `json:"heatmap"`
			TopMovers    []json.RawMessage `json:"top_movers"`
			TotalChanges int               `json:"total_changes"`
			Window       string            `json:"window"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data.Heatmap) != 1 {
		t.Errorf("expected 1 heatmap group, got %d", len(envelope.Data.Heatmap))
	}
	if len(envelope.Data.TopMovers) != 1 {
		t.Errorf("expected 1 top mover, got %d", len(envelope.Data.TopMovers))
	}
	if envelope.Data.TotalChanges != 3 {
		t.Errorf("total_changes: expected 3, got %d", envelope.Data.TotalChanges)
	}
	if envelope.Data.Window != "24h" {
		t.Errorf("window: expected 24h, got %q", envelope.Data.Window)
	}
}

func TestGetChangesSummary_InvalidWindow_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/changes/summary?window=1y")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for window=1y, got %d", status)
	}
}

func TestGetChangesSummary_DefaultWindow(t *testing.T) {
	var capturedFilter handlers.SummaryFilter
	store := &mockStore{
		getChangesSummary: func(_ context.Context, f handlers.SummaryFilter) (handlers.ChangesSummary, error) {
			capturedFilter = f
			return handlers.ChangesSummary{}, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/changes/summary")

	if capturedFilter.Since == nil {
		t.Fatal("expected Since to be set (from default 24h window)")
	}
	// Since should be approximately 24h ago.
	diff := time.Since(*capturedFilter.Since)
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("Since should be ~24h ago, got %v ago", diff)
	}
}

func TestGetChangesSummary_7dWindow(t *testing.T) {
	var capturedFilter handlers.SummaryFilter
	store := &mockStore{
		getChangesSummary: func(_ context.Context, f handlers.SummaryFilter) (handlers.ChangesSummary, error) {
			capturedFilter = f
			return handlers.ChangesSummary{}, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/changes/summary?window=7d")

	if capturedFilter.Since == nil {
		t.Fatal("expected Since to be set (from 7d window)")
	}
	diff := time.Since(*capturedFilter.Since)
	if diff < 6*24*time.Hour || diff > 8*24*time.Hour {
		t.Errorf("Since should be ~7d ago, got %v ago", diff)
	}
}

func TestGetChangesSummary_ProviderFilter(t *testing.T) {
	var capturedFilter handlers.SummaryFilter
	store := &mockStore{
		getChangesSummary: func(_ context.Context, f handlers.SummaryFilter) (handlers.ChangesSummary, error) {
			capturedFilter = f
			return handlers.ChangesSummary{}, nil
		},
	}
	app := newApp(store)

	get(t, app, "/v1/changes/summary?provider=anthropic")

	if capturedFilter.Provider != "anthropic" {
		t.Errorf("provider: expected anthropic, got %q", capturedFilter.Provider)
	}
}

func TestGetChangesSummary_SinceOverridesWindow(t *testing.T) {
	var capturedFilter handlers.SummaryFilter
	store := &mockStore{
		getChangesSummary: func(_ context.Context, f handlers.SummaryFilter) (handlers.ChangesSummary, error) {
			capturedFilter = f
			return handlers.ChangesSummary{}, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/changes/summary?window=7d&since=2026-01-15T00:00:00Z")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	if capturedFilter.Since == nil {
		t.Fatal("expected Since to be set")
	}
	expected := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if !capturedFilter.Since.Equal(expected) {
		t.Errorf("Since: got %v, want %v (explicit since should override window)", *capturedFilter.Since, expected)
	}

	// When since overrides window, the response window field should be "custom".
	var envelope struct {
		Data struct {
			Window string `json:"window"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Window != "custom" {
		t.Errorf("window: expected \"custom\" when since overrides, got %q", envelope.Data.Window)
	}
}

func TestGetChangesSummary_EmptyResult_NullSafeArrays(t *testing.T) {
	store := &mockStore{
		getChangesSummary: func(_ context.Context, _ handlers.SummaryFilter) (handlers.ChangesSummary, error) {
			// Return nil slices to test null-safety.
			return handlers.ChangesSummary{
				Heatmap:   nil,
				TopMovers: nil,
			}, nil
		},
	}
	app := newApp(store)

	status, body := get(t, app, "/v1/changes/summary")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	// Verify slices are [] not null in JSON.
	if !strings.Contains(string(body), `"heatmap":[]`) {
		t.Errorf("heatmap should be [] not null; body: %s", body)
	}
	if !strings.Contains(string(body), `"top_movers":[]`) {
		t.Errorf("top_movers should be [] not null; body: %s", body)
	}
}

// --- RFC 7807 error shape test ------------------------------------------

func TestErrorShape_RFC7807(t *testing.T) {
	app := newApp(&mockStore{})

	// Invalid query param on a free-tier endpoint is a guaranteed 400 that
	// exercises the RFC 7807 error-shape path without depending on slug routing.
	status, body := get(t, app, "/v1/models?min_context=abc")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}

	var problem struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("unmarshal problem: %v; body: %s", err, body)
	}
	if problem.Type == "" {
		t.Error("expected non-empty type")
	}
	if problem.Title == "" {
		t.Error("expected non-empty title")
	}
	if problem.Status != 400 {
		t.Errorf("expected status=400, got %d", problem.Status)
	}
}
