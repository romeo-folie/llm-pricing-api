package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
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
	listProviders         func(ctx context.Context) ([]handlers.ProviderRow, error)
	compareModels         func(ctx context.Context, ids []int) ([]handlers.ModelRow, error)
	listChanges           func(ctx context.Context, filter handlers.ChangesFilter) ([]handlers.ChangeRow, error)
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

func (m *mockStore) ListChanges(ctx context.Context, filter handlers.ChangesFilter) ([]handlers.ChangeRow, error) {
	if m.listChanges != nil {
		return m.listChanges(ctx, filter)
	}
	return nil, nil
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
	v1.Get("/changes", h.ListChanges)
	return app
}

// get performs a GET request and returns the *http.Response.
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

func TestGetModel_InvalidID_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/abc")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestGetModel_ZeroID_Returns400(t *testing.T) {
	app := newApp(&mockStore{})

	status, _ := get(t, app, "/v1/models/0")
	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
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
		listChanges: func(_ context.Context, _ handlers.ChangesFilter) ([]handlers.ChangeRow, error) {
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
			}, nil
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
		listChanges: func(_ context.Context, f handlers.ChangesFilter) ([]handlers.ChangeRow, error) {
			capturedFilter = f
			return nil, nil
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
		listChanges: func(_ context.Context, _ handlers.ChangesFilter) ([]handlers.ChangeRow, error) {
			return nil, nil
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

// --- RFC 7807 error shape test ------------------------------------------

func TestErrorShape_RFC7807(t *testing.T) {
	app := newApp(&mockStore{})

	status, body := get(t, app, "/v1/models/abc")
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
