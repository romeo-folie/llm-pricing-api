package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
)

// mockFeedbackStore implements handlers.FeedbackStore for testing.
type mockFeedbackStore struct {
	modelSlugExists   func(ctx context.Context, slug string) (bool, error)
	isDuplicateFeed   func(ctx context.Context, sessionID, modelSlug, useCase string) (bool, error)
	insertFeedback    func(ctx context.Context, f handlers.FeedbackRow) error
	topFeedbackModels func(ctx context.Context, useCase string, days, limit int, positive bool) ([]handlers.FeedbackSummary, error)
	getEffWeights     func(ctx context.Context, useCase string) (map[string]map[string]float64, error)
}

func (m *mockFeedbackStore) ModelSlugExists(ctx context.Context, slug string) (bool, error) {
	if m.modelSlugExists != nil {
		return m.modelSlugExists(ctx, slug)
	}
	return true, nil
}

func (m *mockFeedbackStore) IsDuplicateFeedback(ctx context.Context, sessionID, modelSlug, useCase string) (bool, error) {
	if m.isDuplicateFeed != nil {
		return m.isDuplicateFeed(ctx, sessionID, modelSlug, useCase)
	}
	return false, nil
}

func (m *mockFeedbackStore) InsertFeedback(ctx context.Context, f handlers.FeedbackRow) error {
	if m.insertFeedback != nil {
		return m.insertFeedback(ctx, f)
	}
	return nil
}

func (m *mockFeedbackStore) TopFeedbackModels(ctx context.Context, useCase string, days, limit int, positive bool) ([]handlers.FeedbackSummary, error) {
	if m.topFeedbackModels != nil {
		return m.topFeedbackModels(ctx, useCase, days, limit, positive)
	}
	return nil, nil
}

func (m *mockFeedbackStore) GetEffectiveWeights(ctx context.Context, useCase string) (map[string]map[string]float64, error) {
	if m.getEffWeights != nil {
		return m.getEffWeights(ctx, useCase)
	}
	return nil, nil
}

func newFeedbackApp(store handlers.FeedbackStore) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	fh := handlers.NewFeedbackHandler(store)
	app.Post("/v1/feedback", fh.Create)
	return app
}

func TestFeedback_HappyPath(t *testing.T) {
	var inserted bool
	store := &mockFeedbackStore{
		insertFeedback: func(_ context.Context, f handlers.FeedbackRow) error {
			inserted = true
			if f.UseCase != "coding" || f.ModelSlug != "openai/gpt-4o" || f.Signal != 1 {
				t.Errorf("unexpected feedback row: %+v", f)
			}
			return nil
		},
	}
	app := newFeedbackApp(store)

	body := `{"use_case":"coding","model_slug":"openai/gpt-4o","signal":1,"session_id":"abc-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(b))
	}
	if !inserted {
		t.Fatal("expected InsertFeedback to be called")
	}
}

func TestFeedback_UnknownModelSlug(t *testing.T) {
	store := &mockFeedbackStore{
		modelSlugExists: func(_ context.Context, slug string) (bool, error) {
			return false, nil
		},
	}
	app := newFeedbackApp(store)

	body := `{"use_case":"coding","model_slug":"unknown/model","signal":1,"session_id":"abc-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFeedback_InvalidUseCase(t *testing.T) {
	store := &mockFeedbackStore{}
	app := newFeedbackApp(store)

	body := `{"use_case":"invalid_uc","model_slug":"openai/gpt-4o","signal":1,"session_id":"abc-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFeedback_InvalidSignal(t *testing.T) {
	store := &mockFeedbackStore{}
	app := newFeedbackApp(store)

	body := `{"use_case":"coding","model_slug":"openai/gpt-4o","signal":2,"session_id":"abc-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFeedback_DuplicateWithin1h(t *testing.T) {
	store := &mockFeedbackStore{
		isDuplicateFeed: func(_ context.Context, _, _, _ string) (bool, error) {
			return true, nil
		},
	}
	app := newFeedbackApp(store)

	body := `{"use_case":"coding","model_slug":"openai/gpt-4o","signal":1,"session_id":"abc-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestFeedback_MissingSessionID(t *testing.T) {
	store := &mockFeedbackStore{}
	app := newFeedbackApp(store)

	body := `{"use_case":"coding","model_slug":"openai/gpt-4o","signal":1}`
	req := httptest.NewRequest(http.MethodPost, "/v1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Admin feedback tests ---

func newAdminApp(store handlers.FeedbackStore) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	ah := handlers.NewAdminFeedbackHandler(store)
	app.Get("/admin/feedback", ah.GetAnalytics)
	return app
}

func TestAdminFeedback_EmptyResults(t *testing.T) {
	store := &mockFeedbackStore{}
	app := newAdminApp(store)

	req := httptest.NewRequest(http.MethodGet, "/admin/feedback?use_case=coding&days=30", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var envelope struct {
		Data struct {
			UseCase      string                        `json:"use_case"`
			PeriodDays   int                           `json:"period_days"`
			TopUpvoted   []handlers.FeedbackSummary    `json:"top_upvoted"`
			TopDownvoted []handlers.FeedbackSummary    `json:"top_downvoted"`
			Weights      map[string]map[string]float64 `json:"effective_weights"`
		} `json:"data"`
	}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("json decode: %v\nbody: %s", err, string(b))
	}

	if envelope.Data.UseCase != "coding" {
		t.Errorf("expected use_case=coding, got %s", envelope.Data.UseCase)
	}
	if len(envelope.Data.TopUpvoted) != 0 {
		t.Errorf("expected empty top_upvoted, got %d", len(envelope.Data.TopUpvoted))
	}
	if len(envelope.Data.TopDownvoted) != 0 {
		t.Errorf("expected empty top_downvoted, got %d", len(envelope.Data.TopDownvoted))
	}
	// Should have hardcoded weights fallback.
	if len(envelope.Data.Weights) == 0 {
		t.Error("expected effective_weights to be populated with hardcoded fallback")
	}
}

func TestAdminFeedback_InvalidUseCase(t *testing.T) {
	store := &mockFeedbackStore{}
	app := newAdminApp(store)

	req := httptest.NewRequest(http.MethodGet, "/admin/feedback?use_case=notreal", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
