//go:build integration

package handlers_test

// Integration test suite for all nine /v1/ API endpoints.
//
// Run with:
//
//	go test ./internal/api/handlers/... -tags integration -v -run Integration
//
// Prerequisites:
//   - PostgreSQL (TimescaleDB) reachable at DATABASE_URL (default: localhost:5434)
//   - Redis reachable at REDIS_URL (default: localhost:6380)
//   - Migrations already applied to the test database
//
// The suite uses a real DB and Redis but injects a mock UnkeyVerifier so no
// real Unkey API calls are made.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/cache"
	"llm-pricing-api/internal/middleware"
)

// -----------------------------------------------------------------------
// Mock UnkeyVerifier — no real Unkey API calls
// -----------------------------------------------------------------------

type mockUnkeyVerifier struct{}

// VerifyKey returns deterministic results for three test API keys.
// Any other key is treated as invalid.
func (m *mockUnkeyVerifier) VerifyKey(_ context.Context, key, _ string) (bool, string, error) {
	switch key {
	case "test-free-key":
		return true, middleware.TierFree, nil
	case "test-dev-key":
		return true, middleware.TierDeveloper, nil
	case "test-pro-key":
		return true, middleware.TierPro, nil
	default:
		return false, "", nil
	}
}

// -----------------------------------------------------------------------
// Test app setup helpers
// -----------------------------------------------------------------------

// databaseURL returns the integration test DB URL.
// Prefer DATABASE_URL env var; fall back to docker-compose defaults.
func databaseURL() string {
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://llmpricing:llmpricing@localhost:5434/llmpricing"
}

// redisURL returns the integration test Redis URL.
// Prefer REDIS_URL env var; fall back to docker-compose defaults.
func redisURL() string {
	if u := os.Getenv("REDIS_URL"); u != "" {
		return u
	}
	return "redis://localhost:6380"
}

// applySeed reads testdata/seed.sql (relative to the repo root) and executes
// it against the provided DB pool. It is idempotent: TRUNCATE + re-insert.
func applySeed(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	// `go test` sets the working directory to the package directory
	// (internal/api/handlers/). testdata/seed.sql lives at the repo root,
	// three levels up.
	seedPath := "../../../testdata/seed.sql"

	data, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed.sql: %v (tried %s)", err, seedPath)
	}

	if _, err := db.Exec(context.Background(), string(data)); err != nil {
		t.Fatalf("apply seed.sql: %v", err)
	}
}

// flushTestRedisKeys deletes all rate-limit and cache keys that were written by
// the integration test API keys. This isolates tests that share a Redis instance.
func flushTestRedisKeys(t *testing.T, ctx context.Context, rdb *redis.Client) {
	t.Helper()

	for _, pattern := range []string{"ratelimit:*", "cache:*", "unkey:*"} {
		var cursor uint64
		for {
			keys, next, err := rdb.Scan(ctx, cursor, pattern, 500).Result()
			if err != nil {
				break
			}
			if len(keys) > 0 {
				if err := rdb.Del(ctx, keys...).Err(); err != nil {
					t.Logf("warning: failed to delete redis keys %v: %v", keys, err)
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
}

// setupTestApp connects to the real DB and Redis, applies the seed, flushes
// test Redis keys, registers all route groups (Free, Dev, Pro, Webhooks, SSE,
// Discovery), and returns the configured Fiber app.
func setupTestApp(t *testing.T) *fiber.App {
	t.Helper()

	ctx := context.Background()

	// ---- database ----
	dbURL := databaseURL()
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("ping db: %v (is the database running? DATABASE_URL=%s)", err, dbURL)
	}

	// ---- redis ----
	rURL := redisURL()
	rdb, err := cache.Connect(ctx, rURL)
	if err != nil {
		t.Fatalf("connect to redis: %v (is redis running? REDIS_URL=%s)", err, rURL)
	}

	// Flush stale rate-limit / cache / unkey verification entries so that
	// tests don't interfere with each other when sharing a Redis instance.
	flushTestRedisKeys(t, ctx, rdb)

	// ---- seed ----
	applySeed(t, db)

	t.Cleanup(func() {
		applySeed(t, db) // reset DB state for subsequent test isolation
		flushTestRedisKeys(t, ctx, rdb)
		rdb.Close()
		db.Close()
	})

	// ---- build app ----
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})

	verifier := &mockUnkeyVerifier{}
	v1 := app.Group("/v1",
		middleware.Auth(verifier, rdb, "test-api-id"),
		middleware.RateLimit(rdb),
		middleware.Cache(rdb),
	)

	handlers.RegisterFree(v1, db, rdb)
	handlers.RegisterDev(v1, db, rdb)
	log := zerolog.Nop()
	// Pass "" as webhookSecretKey: the handler skips AES-256-GCM when no
	// 32-byte key is configured and stores secrets as plaintext. Intentional
	// for tests — never use an empty key in production.
	handlers.RegisterPro(v1, db, rdb, "", log)
	if err := handlers.RegisterSSE(v1); err != nil {
		t.Fatalf("register SSE: %v", err)
	}
	handlers.RegisterDiscovery(app, db)

	return app
}

// -----------------------------------------------------------------------
// Request helpers
// -----------------------------------------------------------------------

// apiGet performs a GET request with the supplied Authorization header value
// and returns the status code and body bytes.
func apiGet(t *testing.T, app *fiber.App, path, authHeader string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req, 10_000 /* 10 s timeout */)
	if err != nil {
		t.Fatalf("app.Test GET %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, body
}

// apiPost performs a POST request with JSON body and Authorization header.
func apiPost(t *testing.T, app *fiber.App, path, authHeader string, payload any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test POST %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, body
}

// apiDelete performs a DELETE request with Authorization header.
func apiDelete(t *testing.T, app *fiber.App, path, authHeader string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("DELETE", path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test DELETE %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, body
}

// freeAuth / devAuth / proAuth are shorthand Authorization header values.
// Most tests use devAuth to avoid exhausting the free-tier rate limit (100 req/day).
// Tests that explicitly exercise free-tier behaviour (rate limiting, tier gating)
// use freeAuth.
const (
	freeAuth = "Bearer test-free-key"
	devAuth  = "Bearer test-dev-key"
	proAuth  = "Bearer test-pro-key"
)

// -----------------------------------------------------------------------
// 1. Auth tests
// -----------------------------------------------------------------------

func TestIntegrationAuth_MissingHeader_Returns401(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models", "")

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 401)
}

func TestIntegrationAuth_InvalidKey_Returns401(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models", "Bearer totally-invalid-key")

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 401)
}

func TestIntegrationAuth_ValidFreeKey_Returns200(t *testing.T) {
	app := setupTestApp(t)

	// A single request with the free key must succeed (well within the 100/day limit).
	status, body := apiGet(t, app, "/v1/models", freeAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200 with valid free key, got %d; body: %s", status, body)
	}
}

// -----------------------------------------------------------------------
// 2. Tier gating tests
// -----------------------------------------------------------------------

func TestIntegrationTierGating_FreeKey_HistoryEndpoint_Returns403(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models/1/history", freeAuth)

	assertTierGating403(t, status, body, middleware.TierDeveloper)
}

func TestIntegrationTierGating_FreeKey_RecommendEndpoint_Returns403(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/recommend", freeAuth)

	assertTierGating403(t, status, body, middleware.TierDeveloper)
}

func TestIntegrationTierGating_FreeKey_ContextEndpoint_Returns403(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/context", freeAuth)

	assertTierGating403(t, status, body, middleware.TierDeveloper)
}

func TestIntegrationTierGating_FreeKey_WebhooksEndpoint_Returns403(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/webhooks", freeAuth, map[string]string{"url": "https://example.com/hook"})

	assertTierGating403(t, status, body, middleware.TierPro)
}

// assertTierGating403 verifies the response is a 403 RFC 7807 problem detail
// with a tier_required extension field set to the expected tier.
func assertTierGating403(t *testing.T, status int, body []byte, expectedTier string) {
	t.Helper()

	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", status, body)
	}

	var problem struct {
		Type       string         `json:"type"`
		Title      string         `json:"title"`
		Status     int            `json:"status"`
		Detail     string         `json:"detail"`
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("unmarshal problem: %v; body: %s", err, body)
	}
	if problem.Status != 403 {
		t.Errorf("problem.status: expected 403, got %d", problem.Status)
	}
	if problem.Type == "" {
		t.Error("problem.type must not be empty")
	}
	if problem.Title == "" {
		t.Error("problem.title must not be empty")
	}

	// Verify tier_required extension field.
	if problem.Extensions == nil {
		t.Error("403 response must include extensions with tier_required")
		return
	}
	tierRequired, ok := problem.Extensions["tier_required"]
	if !ok {
		t.Errorf("extensions must contain tier_required; got: %v", problem.Extensions)
		return
	}
	if fmt.Sprintf("%v", tierRequired) != expectedTier {
		t.Errorf("tier_required: expected %q, got %v", expectedTier, tierRequired)
	}
}

// -----------------------------------------------------------------------
// 3. Rate limiting tests
// -----------------------------------------------------------------------

// TestIntegrationRateLimit_FreeKeyExceedsDailyLimit_Returns429 exhausts the
// 100-request free daily limit in one test run. setupTestApp flushes all
// rate-limit / cache / unkey Redis keys on entry, so this test starts from a
// clean counter regardless of run order.
func TestIntegrationRateLimit_FreeKeyExceedsDailyLimit_Returns429(t *testing.T) {
	app := setupTestApp(t)

	// Make exactly 100 requests (the free daily limit).
	for i := 0; i < 100; i++ {
		status, body := apiGet(t, app, "/v1/models", freeAuth)
		if status != fiber.StatusOK {
			t.Fatalf("request %d/%d: expected 200, got %d; body: %s", i+1, 100, status, body)
		}
	}

	// The 101st request must be rate-limited.
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", freeAuth)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("101st request: expected 429, got %d; body: %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 response must include Retry-After header")
	}
}

// -----------------------------------------------------------------------
// 4. Filter tests
// (Use devAuth to avoid hitting the free-key rate limit.)
// -----------------------------------------------------------------------

func TestIntegrationFilters_ProviderFilter_ReturnsOnlyMatchingProvider(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models?provider=openai", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			Provider string `json:"provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("expected at least one openai model")
	}
	for _, m := range envelope.Data {
		if !strings.EqualFold(m.Provider, "openai") {
			t.Errorf("expected provider 'openai', got %q", m.Provider)
		}
	}
}

func TestIntegrationFilters_ModalityFilter_ReturnsOnlyTextModels(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models?modality=text", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			Modality string `json:"modality"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("expected at least one text model")
	}
	for _, m := range envelope.Data {
		if m.Modality != "text" {
			t.Errorf("expected modality 'text', got %q", m.Modality)
		}
	}
}

func TestIntegrationFilters_MinContextFilter_ReturnsOnlyLargeContextModels(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models?min_context=100000", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []struct {
			ContextWindow *int `json:"context_window"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("expected at least one model with context >= 100k")
	}
	for _, m := range envelope.Data {
		if m.ContextWindow == nil || *m.ContextWindow < 100000 {
			got := 0
			if m.ContextWindow != nil {
				got = *m.ContextWindow
			}
			t.Errorf("expected context_window >= 100000, got %d", got)
		}
	}
}

// -----------------------------------------------------------------------
// 5. Compare tests
// -----------------------------------------------------------------------

func TestIntegrationCompare_FiveValidIDs_Returns200(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/compare?models=1,2,3,4,5", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Data) != 5 {
		t.Errorf("expected 5 models, got %d", len(envelope.Data))
	}
}

func TestIntegrationCompare_SixIDs_Returns400(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/compare?models=1,2,3,4,5,6", devAuth)

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 400)
}

func TestIntegrationCompare_UnknownModelID_Returns404(t *testing.T) {
	app := setupTestApp(t)

	// Model 99999 does not exist in the seed data.
	status, body := apiGet(t, app, "/v1/compare?models=1,99999", devAuth)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 404)
}

// -----------------------------------------------------------------------
// 6. Pagination tests
// -----------------------------------------------------------------------

func TestIntegrationPagination_SecondPage_ReturnsCorrectSlice(t *testing.T) {
	app := setupTestApp(t)

	// Page 1 with per_page=5.
	status1, body1 := apiGet(t, app, "/v1/models?page=1&per_page=5", devAuth)
	if status1 != fiber.StatusOK {
		t.Fatalf("page 1: expected 200, got %d; body: %s", status1, body1)
	}
	var env1 struct {
		Data []struct{ ID int `json:"id"` } `json:"data"`
	}
	if err := json.Unmarshal(body1, &env1); err != nil {
		t.Fatalf("unmarshal page 1: %v", err)
	}

	// Page 2 — use a raw request so we can inspect response headers.
	req2 := httptest.NewRequest("GET", "/v1/models?page=2&per_page=5", nil)
	req2.Header.Set("Authorization", devAuth)
	resp2, err := app.Test(req2, 10_000)
	if err != nil {
		t.Fatalf("app.Test page 2: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if resp2.StatusCode != fiber.StatusOK {
		t.Fatalf("page 2: expected 200, got %d; body: %s", resp2.StatusCode, body2)
	}

	// X-Total-Count must be present.
	if resp2.Header.Get("X-Total-Count") == "" {
		t.Error("X-Total-Count header must be present on paginated responses")
	}

	var env2 struct {
		Data []struct{ ID int `json:"id"` } `json:"data"`
	}
	if err := json.Unmarshal(body2, &env2); err != nil {
		t.Fatalf("unmarshal page 2: %v", err)
	}

	// The two pages must not overlap.
	page1IDs := make(map[int]struct{}, len(env1.Data))
	for _, m := range env1.Data {
		page1IDs[m.ID] = struct{}{}
	}
	for _, m := range env2.Data {
		if _, dup := page1IDs[m.ID]; dup {
			t.Errorf("model id=%d appears on both page 1 and page 2", m.ID)
		}
	}
}

// -----------------------------------------------------------------------
// 7. Trust metadata tests
// -----------------------------------------------------------------------

func TestIntegrationTrustMetadata_ModelResponse_IncludesAllTrustFields(t *testing.T) {
	app := setupTestApp(t)

	// Model 1 has price_history rows with two sources — should yield high confidence.
	status, body := apiGet(t, app, "/v1/models/1", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	// The per-model meta is embedded inside the model's "meta" field AND also
	// returned as the top-level envelope meta. Both are TrustMeta.
	// Verify the top-level envelope meta.
	var envelope struct {
		Data json.RawMessage `json:"data"`
		Meta struct {
			ConfirmedAt    *time.Time `json:"confirmed_at"`
			Source         string     `json:"source"`
			Confidence     string     `json:"confidence"`
			AgeHours       *float64   `json:"age_hours"`
			ChangeVelocity *float64   `json:"change_velocity"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}

	if envelope.Meta.ConfirmedAt == nil {
		t.Error("meta.confirmed_at must be present")
	}
	if envelope.Meta.Source == "" {
		t.Error("meta.source must be non-empty")
	}
	if envelope.Meta.Confidence == "" {
		t.Error("meta.confidence must be non-empty")
	}
	if envelope.Meta.AgeHours == nil {
		t.Error("meta.age_hours must be present")
	}
	if envelope.Meta.ChangeVelocity == nil {
		t.Error("meta.change_velocity must be present")
	}

	// Also verify the embedded model.meta field has the same trust fields.
	var dataWrapper struct {
		Meta struct {
			ConfirmedAt    *time.Time `json:"confirmed_at"`
			Source         string     `json:"source"`
			Confidence     string     `json:"confidence"`
			AgeHours       *float64   `json:"age_hours"`
			ChangeVelocity *float64   `json:"change_velocity"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(envelope.Data, &dataWrapper); err != nil {
		t.Fatalf("unmarshal data.meta: %v", err)
	}
	if dataWrapper.Meta.Source == "" {
		t.Error("data.meta.source must be non-empty")
	}
}

func TestIntegrationTrustMetadata_ListResponse_MetaPresent(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Meta struct {
			ConfirmedAt    json.RawMessage `json:"confirmed_at"`
			Source         json.RawMessage `json:"source"`
			Confidence     json.RawMessage `json:"confidence"`
			AgeHours       json.RawMessage `json:"age_hours"`
			ChangeVelocity json.RawMessage `json:"change_velocity"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if len(envelope.Meta.Confidence) == 0 {
		t.Error("meta.confidence must be present in list response")
	}
	if len(envelope.Meta.AgeHours) == 0 {
		t.Error("meta.age_hours must be present in list response")
	}
	if len(envelope.Meta.ChangeVelocity) == 0 {
		t.Error("meta.change_velocity must be present in list response")
	}
}

// -----------------------------------------------------------------------
// 8. RFC 7807 error tests
// -----------------------------------------------------------------------

func TestIntegrationRFC7807_AllErrorResponsesHaveProblemContentType(t *testing.T) {
	app := setupTestApp(t)

	cases := []struct {
		name       string
		method     string
		path       string
		auth       string
		wantStatus int
	}{
		{"no auth header", "GET", "/v1/models", "", 401},
		{"invalid key", "GET", "/v1/models", "Bearer bad-key", 401},
		{"invalid model id", "GET", "/v1/models/0", devAuth, 400},
		{"model not found", "GET", "/v1/models/99999", devAuth, 404},
		{"too many compare ids", "GET", "/v1/compare?models=1,2,3,4,5,6", devAuth, 400},
		{"free tier on dev endpoint", "GET", "/v1/models/1/history", freeAuth, 403},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := app.Test(req, 10_000)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %d, got %d; body: %s", tc.wantStatus, resp.StatusCode, body)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/problem+json") {
				t.Errorf("Content-Type: expected application/problem+json, got %q", ct)
			}

			assertProblemJSON(t, body, tc.wantStatus)
		})
	}
}

// assertProblemJSON verifies the body is valid RFC 7807 with the expected status.
func assertProblemJSON(t *testing.T, body []byte, expectedStatus int) {
	t.Helper()

	var problem struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("problem detail is not valid JSON: %v; body: %s", err, body)
	}
	if problem.Type == "" {
		t.Error("problem.type must not be empty")
	}
	if problem.Title == "" {
		t.Error("problem.title must not be empty")
	}
	if problem.Status != expectedStatus {
		t.Errorf("problem.status: expected %d, got %d", expectedStatus, problem.Status)
	}
}

// -----------------------------------------------------------------------
// 9. Webhook tests
// -----------------------------------------------------------------------

func TestIntegrationWebhooks_ProKey_Create_Returns201(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/webhooks", proAuth, map[string]string{
		"url": "https://example.com/webhook",
	})

	if status != fiber.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			ID     string `json:"id"`
			URL    string `json:"url"`
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal 201 body: %v; body: %s", err, body)
	}
	if envelope.Data.ID == "" {
		t.Error("webhook id must not be empty")
	}
	if envelope.Data.URL != "https://example.com/webhook" {
		t.Errorf("webhook url: expected %q, got %q", "https://example.com/webhook", envelope.Data.URL)
	}
	if envelope.Data.Secret == "" {
		t.Error("webhook secret must be returned in 201 response")
	}
}

func TestIntegrationWebhooks_ProKey_Delete_Returns204(t *testing.T) {
	app := setupTestApp(t)

	// First create a webhook.
	createStatus, createBody := apiPost(t, app, "/v1/webhooks", proAuth, map[string]string{
		"url": "https://example.com/hook-to-delete",
	})
	if createStatus != fiber.StatusCreated {
		t.Fatalf("create webhook: expected 201, got %d; body: %s", createStatus, createBody)
	}

	var createResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createBody, &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	// Delete it.
	deleteStatus, deleteBody := apiDelete(t, app, "/v1/webhooks/"+createResp.Data.ID, proAuth)

	if deleteStatus != fiber.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", deleteStatus, deleteBody)
	}
}

func TestIntegrationWebhooks_FreeKey_Create_Returns403(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/webhooks", freeAuth, map[string]string{
		"url": "https://example.com/webhook",
	})

	assertTierGating403(t, status, body, middleware.TierPro)
}

// -----------------------------------------------------------------------
// 10. Context token budget test
// -----------------------------------------------------------------------

func TestIntegrationContextTokenBudget_ResponseWithin2100Tokens(t *testing.T) {
	app := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/context", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	// Use the same 4-chars-per-token approximation as the handler.
	approxTokens := (len(body) + 3) / 4
	if approxTokens > 2100 {
		t.Errorf("/v1/context response exceeds 2100-token budget: ~%d tokens (%d bytes)",
			approxTokens, len(body))
	}
	t.Logf("/v1/context: %d bytes, ~%d tokens", len(body), approxTokens)
}
