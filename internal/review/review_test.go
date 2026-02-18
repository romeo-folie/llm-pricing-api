package review_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/review"
)

// ----- mockStore -----

type mockStore struct {
	pendingItems []review.ReviewItem
	listErr      error
	approveErr   error
	rejectErr    error

	approvedID int
	rejectedID int
}

func (m *mockStore) ListPending(_ context.Context) ([]review.ReviewItem, error) {
	return m.pendingItems, m.listErr
}

func (m *mockStore) Approve(_ context.Context, id int) error {
	m.approvedID = id
	return m.approveErr
}

func (m *mockStore) Reject(_ context.Context, id int) error {
	m.rejectedID = id
	return m.rejectErr
}

// ----- helpers -----

func newApp(store review.Store) *fiber.App {
	app := fiber.New(fiber.Config{
		// Disable startup banner in tests.
		DisableStartupMessage: true,
	})
	h := review.NewHandler(store)
	app.Get("/admin/review", h.List)
	app.Post("/admin/review/:id/approve", h.Approve)
	app.Post("/admin/review/:id/reject", h.Reject)
	return app
}

func sampleItem() review.ReviewItem {
	return review.ReviewItem{
		ID:          42,
		ModelID:     1,
		ModelName:   "GPT-4o",
		Provider:    "openai",
		Field:       models.PriceFieldInput,
		SourceAID:   1,
		SourceAName: "openrouter",
		SourceBID:   2,
		SourceBName: "litellm",
		ValueA:      0.000005,
		ValueB:      0.000010,
		DeltaPct:    0.5,
		Status:      models.ReviewStatusPending,
		CreatedAt:   time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC),
	}
}

// ----- list tests -----

// TestList_Empty verifies that an empty store returns 200 with a "No pending items" message.
func TestList_Empty(t *testing.T) {
	store := &mockStore{pendingItems: []review.ReviewItem{}}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodGet, "/admin/review", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if !strings.Contains(body, "No pending items") {
		t.Errorf("expected body to contain 'No pending items'; got:\n%s", body)
	}
}

// TestList_WithItems verifies that pending items are rendered in the response.
func TestList_WithItems(t *testing.T) {
	item := sampleItem()
	store := &mockStore{pendingItems: []review.ReviewItem{item}}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodGet, "/admin/review", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if !strings.Contains(body, item.ModelName) {
		t.Errorf("expected body to contain model name %q; got:\n%s", item.ModelName, body)
	}
	if !strings.Contains(body, string(item.Field)) {
		t.Errorf("expected body to contain field %q; got:\n%s", item.Field, body)
	}
}

// TestList_StoreError verifies that a store error returns HTTP 500.
func TestList_StoreError(t *testing.T) {
	store := &mockStore{listErr: errors.New("db connection lost")}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodGet, "/admin/review", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// ----- approve tests -----

// TestApprove_Success verifies that a valid id triggers a 303 redirect.
func TestApprove_Success(t *testing.T) {
	store := &mockStore{}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/review/42/approve", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected status 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/admin/review" {
		t.Errorf("expected redirect to /admin/review, got %q", loc)
	}
	if store.approvedID != 42 {
		t.Errorf("expected Approve called with id=42, got %d", store.approvedID)
	}
}

// TestApprove_InvalidID verifies that a non-numeric id returns 400.
func TestApprove_InvalidID(t *testing.T) {
	cases := []string{"not-a-number", "0", "-1"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			store := &mockStore{}
			app := newApp(store)

			req := httptest.NewRequest(http.MethodPost, "/admin/review/"+raw+"/approve", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("id=%q: expected status 400, got %d", raw, resp.StatusCode)
			}
		})
	}
}

// TestApprove_NotPending verifies that ErrNotPending from the store returns 409 Conflict.
func TestApprove_NotPending(t *testing.T) {
	store := &mockStore{approveErr: review.ErrNotPending}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/review/7/approve", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 for ErrNotPending, got %d", resp.StatusCode)
	}
}

// TestApprove_StoreError verifies that a store error returns HTTP 500.
func TestApprove_StoreError(t *testing.T) {
	store := &mockStore{approveErr: errors.New("tx rollback")}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/review/1/approve", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// ----- reject tests -----

// TestReject_Success verifies that a valid id triggers a 303 redirect.
func TestReject_Success(t *testing.T) {
	store := &mockStore{}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/review/99/reject", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected status 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/admin/review" {
		t.Errorf("expected redirect to /admin/review, got %q", loc)
	}
	if store.rejectedID != 99 {
		t.Errorf("expected Reject called with id=99, got %d", store.rejectedID)
	}
}

// TestReject_InvalidID verifies that a non-numeric or non-positive id returns 400.
func TestReject_InvalidID(t *testing.T) {
	cases := []string{"abc", "0", "-5"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			store := &mockStore{}
			app := newApp(store)

			req := httptest.NewRequest(http.MethodPost, "/admin/review/"+raw+"/reject", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("id=%q: expected status 400, got %d", raw, resp.StatusCode)
			}
		})
	}
}

// TestReject_NotPending verifies that ErrNotPending from the store returns 409 Conflict.
func TestReject_NotPending(t *testing.T) {
	store := &mockStore{rejectErr: review.ErrNotPending}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/review/7/reject", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 for ErrNotPending, got %d", resp.StatusCode)
	}
}

// TestReject_StoreError verifies that a store error returns HTTP 500.
func TestReject_StoreError(t *testing.T) {
	store := &mockStore{rejectErr: errors.New("db write failed")}
	app := newApp(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/review/1/reject", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// ----- helpers -----

// readBody reads the response body as a string for assertion.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}
