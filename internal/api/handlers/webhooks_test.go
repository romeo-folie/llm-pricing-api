package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/middleware"
)

// --- mock WebhookStore ---

type mockWebhookStore struct {
	createFunc func(ctx context.Context, apiKeyHash, webhookURL, secret string) (handlers.WebhookRecord, error)
	deleteFunc func(ctx context.Context, id, apiKeyHash string) error
}

func (m *mockWebhookStore) CreateWebhook(ctx context.Context, apiKeyHash, webhookURL, secret string) (handlers.WebhookRecord, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, apiKeyHash, webhookURL, secret)
	}
	return handlers.WebhookRecord{
		ID:        "550e8400-e29b-41d4-a716-446655440000",
		URL:       webhookURL,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockWebhookStore) DeleteWebhook(ctx context.Context, id, apiKeyHash string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id, apiKeyHash)
	}
	return nil
}

// --- helpers ---

// newWebhookApp creates a minimal Fiber app wired with the webhook routes and
// the given WebhookStore, pre-seeded with the tier/key_hash locals.
func newWebhookApp(store handlers.WebhookStore, tier, keyHash string) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})

	// Inject locals directly (simulates auth middleware in tests).
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalKeyTier, tier)
		c.Locals(middleware.LocalKeyHash, keyHash)
		return c.Next()
	})

	v1 := app.Group("/v1")

	// Register only the webhook routes using the supplied mock store.
	wh := &handlers.WebhookHandlerExport{Store: store}
	v1.Post("/webhooks", middleware.RequireTier("pro"), wh.Create)
	v1.Delete("/webhooks/:id", middleware.RequireTier("pro"), wh.Delete)

	return app
}

// doRequest is a helper that fires a request through the test Fiber app and
// returns the status code and response body.
func doRequest(app *fiber.App, method, path string, body io.Reader) (int, []byte) {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		panic("app.Test failed: " + err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// --- tests ---

func TestCreateWebhook_ReturnsSecret_201(t *testing.T) {
	app := newWebhookApp(&mockWebhookStore{}, "pro", "testhash")

	body := bytes.NewBufferString(`{"url":"https://example.com/hook"}`)
	status, respBody := doRequest(app, "POST", "/v1/webhooks", body)

	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", status, respBody)
	}

	var env struct {
		Data struct {
			ID        string `json:"id"`
			URL       string `json:"url"`
			CreatedAt string `json:"created_at"`
			Secret    string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if env.Data.ID == "" {
		t.Error("expected non-empty id in response")
	}
	if env.Data.Secret == "" {
		t.Error("expected secret to be returned once on creation")
	}
	if len(env.Data.Secret) != 64 {
		t.Errorf("expected 64-char hex secret (32 bytes), got len=%d", len(env.Data.Secret))
	}
}

func TestCreateWebhook_InvalidURL_Returns400(t *testing.T) {
	app := newWebhookApp(&mockWebhookStore{}, "pro", "testhash")

	body := bytes.NewBufferString(`{"url":"not-a-url"}`)
	status, _ := doRequest(app, "POST", "/v1/webhooks", body)

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for invalid URL, got %d", status)
	}
}

func TestCreateWebhook_MissingURL_Returns400(t *testing.T) {
	app := newWebhookApp(&mockWebhookStore{}, "pro", "testhash")

	body := bytes.NewBufferString(`{}`)
	status, _ := doRequest(app, "POST", "/v1/webhooks", body)

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for missing URL, got %d", status)
	}
}

func TestCreateWebhook_FreeTier_Returns403(t *testing.T) {
	app := newWebhookApp(&mockWebhookStore{}, "free", "testhash")

	body := bytes.NewBufferString(`{"url":"https://example.com/hook"}`)
	status, respBody := doRequest(app, "POST", "/v1/webhooks", body)

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for free tier, got %d; body: %s", status, respBody)
	}

	var pd api.ProblemDetail
	if err := json.Unmarshal(respBody, &pd); err != nil {
		t.Fatalf("unmarshal problem detail: %v", err)
	}
	if pd.Status != fiber.StatusForbidden {
		t.Errorf("expected status=403 in problem detail, got %d", pd.Status)
	}
}

func TestCreateWebhook_DeveloperTier_Returns403(t *testing.T) {
	app := newWebhookApp(&mockWebhookStore{}, "developer", "testhash")

	body := bytes.NewBufferString(`{"url":"https://example.com/hook"}`)
	status, _ := doRequest(app, "POST", "/v1/webhooks", body)

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403 for developer tier, got %d", status)
	}
}

func TestDeleteWebhook_Returns204(t *testing.T) {
	store := &mockWebhookStore{
		deleteFunc: func(_ context.Context, id, _ string) error {
			if id != "test-id" {
				return handlers.ErrWebhookNotFound
			}
			return nil
		},
	}
	app := newWebhookApp(store, "pro", "testhash")

	status, _ := doRequest(app, "DELETE", "/v1/webhooks/test-id", nil)

	if status != fiber.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
}

func TestDeleteWebhook_WrongOwner_Returns404(t *testing.T) {
	store := &mockWebhookStore{
		deleteFunc: func(_ context.Context, _, _ string) error {
			return handlers.ErrWebhookNotFound
		},
	}
	app := newWebhookApp(store, "pro", "differenthash")

	status, _ := doRequest(app, "DELETE", "/v1/webhooks/test-id", nil)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404 for wrong owner, got %d", status)
	}
}
