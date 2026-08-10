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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/api/handlers"
	"llm-pricing-api/internal/cache"
	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/middleware"
	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/reconciler"
	"llm-pricing-api/internal/webhooks"
	"llm-pricing-api/internal/worker"

	"github.com/hibiken/asynq"
)

var update = flag.Bool("update", false, "update goldfile snapshots")

// assertGoldfile compares the given body against a file in testdata/goldfiles.
// If -update is passed, the file is overwritten with the current body.
// Dynamic fields (timestamps, query times) are sanitized before comparison.
func assertGoldfile(t *testing.T, body []byte, filename string) {
	t.Helper()
	path := filepath.Join("testdata", "goldfiles", filename)

	processed := sanitizeJSON(body)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir goldfiles: %v", err)
		}
		if err := os.WriteFile(path, processed, 0644); err != nil {
			t.Fatalf("write goldfile: %v", err)
		}
		t.Logf("updated goldfile: %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read goldfile %s: %v (run with -update to create?)", filename, err)
	}

	if string(processed) != string(want) {
		t.Errorf("goldfile mismatch: %s\nGOT:\n%s\nWANT:\n%s", filename, processed, want)
	}
}

var (
	// Matches ISO8601/RFC3339 timestamps like "2026-03-21T19:27:22.123456Z"
	timestampRegex = regexp.MustCompile(`"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z?"`)
	// Matches query_time_ms: 123
	queryTimeRegex = regexp.MustCompile(`"query_time_ms":\s*\d+`)
	// Matches age_hours: 1.23456
	ageHoursRegex = regexp.MustCompile(`"age_hours":\s*\d+(\.\d+)?`)
)

func sanitizeJSON(b []byte) []byte {
	// 1. Prettify for stable comparison
	var obj any
	if err := json.Unmarshal(b, &obj); err != nil {
		return b // return raw if not JSON
	}
	pretty, _ := json.MarshalIndent(obj, "", "  ")

	// 2. Mask dynamic fields
	res := timestampRegex.ReplaceAll(pretty, []byte(`"<timestamp>"`))
	res = queryTimeRegex.ReplaceAll(res, []byte(`"query_time_ms": 0`))
	res = ageHoursRegex.ReplaceAll(res, []byte(`"age_hours": 0`))
	return res
}

const stableWebhookKeyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// -----------------------------------------------------------------------
// Mock UnkeyVerifier — no real Unkey API calls
// -----------------------------------------------------------------------

type mockUnkeyVerifier struct{}

// VerifyKey returns deterministic results for three test API keys.
// Any other key is treated as invalid.
func (m *mockUnkeyVerifier) VerifyKey(_ context.Context, key, _ string) (bool, string, error) {
	if strings.HasPrefix(key, "test-concurrency-") {
		return true, middleware.TierDeveloper, nil
	}
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

	for _, pattern := range []string{"ratelimit:*", "cache:*", "unkey:*", "asynq:*"} {
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
// Discovery), and returns the configured Fiber app along with its DB and Redis handles.
func setupTestApp(t *testing.T) (*fiber.App, *pgxpool.Pool, *redis.Client) {
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
	v1Opts := []fiber.Handler{
		middleware.Auth(verifier, rdb, "test-api-id"),
	}
	if os.Getenv("SKIP_RATE_LIMIT") == "" {
		v1Opts = append(v1Opts, middleware.RateLimit(rdb))
	}
	v1Opts = append(v1Opts, middleware.Cache(rdb))

	v1 := app.Group("/v1", v1Opts...)

	handlers.RegisterFree(v1, db, rdb)
	if err := handlers.RegisterDev(v1, db, rdb); err != nil {
		t.Fatalf("register dev handlers: %v", err)
	}
	log := zerolog.Nop()
	// Use a stable key for tests so the worker can decrypt what the API encrypted.
	handlers.RegisterPro(v1, db, rdb, stableWebhookKeyHex, log)
	if err := handlers.RegisterSSE(v1, rdb); err != nil {
		t.Fatalf("register SSE: %v", err)
	}
	handlers.RegisterDiscovery(app, db, rdb)

	return app, db, rdb
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
	app, _, _ := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models", "")

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 401)
}

func TestIntegrationAuth_InvalidKey_Returns401(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models", "Bearer totally-invalid-key")

	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 401)
}

func TestIntegrationAuth_ValidFreeKey_Returns200(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// A single request with the free key must succeed (well within the 100/day limit).
	status, body := apiGet(t, app, "/v1/models", freeAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200 with valid free key, got %d; body: %s", status, body)
	}

	assertGoldfile(t, body, "models_list_free.json")
}

// -----------------------------------------------------------------------
// 2. Free-key access tests (tier gating removed for dev endpoints)
// -----------------------------------------------------------------------

func TestIntegrationFreeKey_HistoryEndpoint_Returns200(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models/1/history", freeAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
}

func TestIntegrationFreeKey_RecommendEndpoint_Returns200(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/recommend", freeAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
}

func TestIntegrationFreeKey_ContextEndpoint_Returns200(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/context", freeAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
}

// -----------------------------------------------------------------------
// 3. Rate limiting tests
// -----------------------------------------------------------------------

// TestIntegrationRateLimit_FreeKey_101RequestsStillAllowed verifies free keys
// are no longer capped at 100/day (limit raised to 1M/day).
func TestIntegrationRateLimit_FreeKey_101RequestsStillAllowed(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Make 100 requests; all should succeed well below 1M/day.
	for i := 0; i < 100; i++ {
		status, body := apiGet(t, app, "/v1/models", freeAuth)
		if status != fiber.StatusOK {
			t.Fatalf("request %d/%d: expected 200, got %d; body: %s", i+1, 100, status, body)
		}
	}

	// The 101st request should still succeed.
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", freeAuth)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("101st request: expected 200, got %d; body: %s", resp.StatusCode, body)
	}
}

// -----------------------------------------------------------------------
// 4. Filter tests
// (Use devAuth to avoid hitting the free-key rate limit.)
// -----------------------------------------------------------------------

func TestIntegrationFilters_ProviderFilter_ReturnsOnlyMatchingProvider(t *testing.T) {
	app, _, _ := setupTestApp(t)

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
	app, _, _ := setupTestApp(t)

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
	app, _, _ := setupTestApp(t)

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
	app, _, _ := setupTestApp(t)

	// Use slugs (seed IDs 1–5): gpt-4o, gpt-4o-mini, gpt-4-turbo, text-embedding-3-large, claude-3-5-sonnet
	slugs := "openai/gpt-4o,openai/gpt-4o-mini,openai/gpt-4-turbo,openai/text-embedding-3-large,anthropic/claude-3-5-sonnet"
	status, body := apiGet(t, app, "/v1/compare?models="+slugs, devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	// The compare envelope wraps items under data.items
	var compareEnv struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(envelope.Data, &compareEnv); err != nil {
		t.Fatalf("unmarshal compare envelope: %v; body: %s", err, body)
	}
	if len(compareEnv.Items) != 5 {
		t.Errorf("expected 5 models, got %d", len(compareEnv.Items))
	}
}

func TestIntegrationCompare_SixIDs_Returns400(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Six slugs — exceeds the max of 5.
	slugs := "openai/gpt-4o,openai/gpt-4o-mini,openai/gpt-4-turbo,openai/text-embedding-3-large,anthropic/claude-3-5-sonnet,anthropic/claude-3-haiku"
	status, body := apiGet(t, app, "/v1/compare?models="+slugs, devAuth)

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 400)
}

func TestIntegrationCompare_UnknownModelID_Returns404(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// "nonexistent/model-slug" does not exist in the seed data.
	status, body := apiGet(t, app, "/v1/compare?models=openai/gpt-4o,nonexistent/model-slug", devAuth)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", status, body)
	}

	assertProblemJSON(t, body, 404)
}

// -----------------------------------------------------------------------
// 6. Pagination tests
// -----------------------------------------------------------------------

func TestIntegrationPagination_SecondPage_ReturnsCorrectSlice(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Page 1 with per_page=5.
	status1, body1 := apiGet(t, app, "/v1/models?page=1&per_page=5", devAuth)
	if status1 != fiber.StatusOK {
		t.Fatalf("page 1: expected 200, got %d; body: %s", status1, body1)
	}
	var env1 struct {
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
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
		Data []struct {
			ID int `json:"id"`
		} `json:"data"`
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
	app, _, _ := setupTestApp(t)

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
	app, _, _ := setupTestApp(t)

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
// 8. Slug-based model lookup tests
// -----------------------------------------------------------------------

// TestIntegrationGetModel_BySlug_Returns200 verifies that the handler accepts
// a URL slug (e.g. "openai/gpt-4o") in place of an integer ID.
func TestIntegrationGetModel_BySlug_Returns200(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// openai/gpt-4o is model ID 1 in the seed data.
	status, body := apiGet(t, app, "/v1/models/openai%2Fgpt-4o", devAuth)

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.Slug != "openai/gpt-4o" {
		t.Errorf("expected slug 'openai/gpt-4o', got %q", envelope.Data.Slug)
	}
	if envelope.Data.Name == "" {
		t.Error("model name must not be empty")
	}

	assertGoldfile(t, body, "model_detail_slug.json")
}

// TestIntegrationGetModel_BySlug_NotFound_Returns404 verifies that an unknown
// slug returns 404 with an RFC 7807 body.
func TestIntegrationGetModel_BySlug_NotFound_Returns404(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiGet(t, app, "/v1/models/unknown%2Fslug", devAuth)

	if status != fiber.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", status, body)
	}
	assertProblemJSON(t, body, 404)
}

// -----------------------------------------------------------------------
// 10. RFC 7807 error tests
// -----------------------------------------------------------------------

func TestIntegrationRFC7807_AllErrorResponsesHaveProblemContentType(t *testing.T) {
	app, _, _ := setupTestApp(t)

	cases := []struct {
		name       string
		method     string
		path       string
		auth       string
		wantStatus int
	}{
		{"no auth header", "GET", "/v1/models", "", 401},
		{"invalid key", "GET", "/v1/models", "Bearer bad-key", 401},
		{"model id zero routes to slug not found", "GET", "/v1/models/0", devAuth, 404},
		{"model not found by integer id", "GET", "/v1/models/99999", devAuth, 404},
		{"model not found by slug", "GET", "/v1/models/unknown/slug", devAuth, 404},
		{"too many compare ids", "GET", "/v1/compare?models=openai/gpt-4o,openai/gpt-4o-mini,openai/gpt-4-turbo,openai/text-embedding-3-large,anthropic/claude-3-5-sonnet,anthropic/claude-3-haiku", devAuth, 400},
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
// 11. Webhook tests
// -----------------------------------------------------------------------

func TestIntegrationWebhooks_ProKey_Create_Returns201(t *testing.T) {
	app, _, _ := setupTestApp(t)

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
	app, _, _ := setupTestApp(t)

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

// Webhook registration is not tier-gated — a free-tier key can register one.
func TestIntegrationWebhooks_FreeKey_Create_Returns201(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/webhooks", freeAuth, map[string]string{
		"url": "https://example.com/webhook",
	})

	if status != fiber.StatusCreated {
		t.Fatalf("expected 201 for free-tier key, got %d; body: %s", status, body)
	}
}

// Exercises the cap in real SQL: the 6th active webhook for one key is refused.
// This covers the conditional INSERT, which the handler-level unit test cannot.
func TestIntegrationWebhooks_PerKeyCap_SixthReturns409(t *testing.T) {
	app, _, _ := setupTestApp(t)

	for i := 0; i < 5; i++ {
		status, body := apiPost(t, app, "/v1/webhooks", freeAuth, map[string]string{
			"url": fmt.Sprintf("https://example.com/hook-%d", i),
		})
		if status != fiber.StatusCreated {
			t.Fatalf("webhook %d: expected 201, got %d; body: %s", i, status, body)
		}
	}

	status, body := apiPost(t, app, "/v1/webhooks", freeAuth, map[string]string{
		"url": "https://example.com/hook-overflow",
	})
	if status != fiber.StatusConflict {
		t.Fatalf("6th webhook: expected 409, got %d; body: %s", status, body)
	}

	var problem struct {
		Status     int            `json:"status"`
		Extensions map[string]any `json:"extensions"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("unmarshal problem: %v; body: %s", err, body)
	}
	if problem.Status != fiber.StatusConflict {
		t.Errorf("problem.status: expected 409, got %d", problem.Status)
	}
	if got := problem.Extensions["max_webhooks"]; fmt.Sprintf("%v", got) != "5" {
		t.Errorf("expected max_webhooks=5, got %v", got)
	}
}

// Concurrent registrations must serialize per API key. Without the advisory
// transaction lock, every INSERT can observe the same count and exceed the cap.
func TestIntegrationWebhooks_PerKeyCap_ConcurrentRegistrations(t *testing.T) {
	_, db, _ := setupTestApp(t)

	const attempts = 20
	store := handlers.NewWebhookStore(db)
	start := make(chan struct{})
	results := make(chan error, attempts)

	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := store.CreateWebhook(
				context.Background(),
				"concurrent-cap-test-key",
				fmt.Sprintf("https://example.com/concurrent-%d", i),
				fmt.Sprintf("secret-%d", i),
			)
			results <- err
		}(i)
	}

	close(start)
	wg.Wait()
	close(results)

	created := 0
	limited := 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, handlers.ErrWebhookLimitReached):
			limited++
		default:
			t.Fatalf("unexpected CreateWebhook error: %v", err)
		}
	}

	if created != 5 || limited != attempts-5 {
		t.Fatalf("created=%d limited=%d; want created=5 limited=%d", created, limited, attempts-5)
	}

	var active int
	if err := db.QueryRow(context.Background(), `
		SELECT count(*) FROM webhooks
		WHERE api_key_hash = $1 AND deleted_at IS NULL
	`, "concurrent-cap-test-key").Scan(&active); err != nil {
		t.Fatalf("count active webhooks: %v", err)
	}
	if active != 5 {
		t.Fatalf("active webhooks=%d; want 5", active)
	}
}

// -----------------------------------------------------------------------
// 12. Context token budget test
// -----------------------------------------------------------------------

func TestIntegrationContextTokenBudget_ResponseWithin2100Tokens(t *testing.T) {
	app, _, _ := setupTestApp(t)

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

// -----------------------------------------------------------------------
// 13. /v1/ask integration tests
// -----------------------------------------------------------------------

// TestIntegrationAsk_PriceIntent_Returns200WithIntent verifies that a price-intent
// NL query returns 200 with the correct intent field.
func TestIntegrationAsk_PriceIntent_Returns200WithIntent(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/ask", devAuth, map[string]string{
		"query": "What is the price of gpt-4o?",
	})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Intent              string         `json:"intent"`
			InferredParams      map[string]any `json:"inferred_params"`
			PlainEnglishSummary string         `json:"plain_english_summary"`
			Meta                struct {
				QueryTimeMs int64  `json:"query_time_ms"`
				Parser      string `json:"parser"`
			} `json:"meta"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.Intent != "price" {
		t.Errorf("expected intent 'price', got %q", envelope.Data.Intent)
	}
	if envelope.Data.PlainEnglishSummary == "" {
		t.Error("plain_english_summary must not be empty")
	}
	if envelope.Data.Meta.Parser == "" {
		t.Error("meta.parser must not be empty")
	}

	assertGoldfile(t, body, "ask_price_intent.json")
}

// TestIntegrationAsk_RecommendIntent_Returns200WithRankedModels verifies that a
// recommend-intent query returns ranked_models from the DB.
func TestIntegrationAsk_RecommendIntent_Returns200WithRankedModels(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/ask", devAuth, map[string]string{
		"query": "Recommend the cheapest model for text summarization",
	})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}

	var envelope struct {
		Data struct {
			Intent       string            `json:"intent"`
			RankedModels []json.RawMessage `json:"ranked_models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, body)
	}
	if envelope.Data.Intent != "recommend" {
		t.Errorf("expected intent 'recommend', got %q", envelope.Data.Intent)
	}
	// The seed data contains 6 text-modality models (GPT-4o Mini, GPT-4 Turbo,
	// Claude 3.5 Sonnet, Claude 3 Haiku, Gemini 1.5 Pro, Gemini 1.5 Flash).
	// "summarization" maps to the text modality via taskModalityMap, so at
	// least one ranked model must be returned.
	if len(envelope.Data.RankedModels) == 0 {
		t.Errorf("expected non-empty ranked_models for summarization query; body: %s", body)
	}
}

// TestIntegrationAsk_FreeKey_Returns200 verifies that /v1/ask is accessible to free-tier keys.
func TestIntegrationAsk_FreeKey_Returns200(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/ask", freeAuth, map[string]string{
		"query": "What is the price of gpt-4o?",
	})

	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", status, body)
	}
}

// TestIntegrationAsk_EmptyQuery_Returns400 verifies validation for empty query.
func TestIntegrationAsk_EmptyQuery_Returns400(t *testing.T) {
	app, _, _ := setupTestApp(t)

	status, body := apiPost(t, app, "/v1/ask", devAuth, map[string]string{
		"query": "",
	})

	if status != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", status, body)
	}
	assertProblemJSON(t, body, 400)
}

// -----------------------------------------------------------------------
// 14. /v1/context?format=markdown integration tests
// -----------------------------------------------------------------------

// TestIntegrationContextMarkdown_Returns200WithMarkdownContentType verifies
// that ?format=markdown returns text/markdown content.
func TestIntegrationContextMarkdown_Returns200WithMarkdownContentType(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/v1/context?format=markdown", nil)
	req.Header.Set("Authorization", devAuth)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("expected text/markdown Content-Type, got %q", ct)
	}
}

// TestIntegrationContextMarkdown_ContainsMarkdownTable verifies that the
// markdown response contains the expected table structure and header.
func TestIntegrationContextMarkdown_ContainsMarkdownTable(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/v1/context?format=markdown", nil)
	req.Header.Set("Authorization", devAuth)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	text := string(body)
	// The markdown response always starts with a metadata summary line.
	if !strings.Contains(text, "Models:") {
		t.Errorf("expected 'Models:' header in markdown response, got: %s", text[:intMin(len(text), 200)])
	}
	// The table must have a header row with known columns.
	if !strings.Contains(text, "| Model |") && !strings.Contains(text, "|Model|") {
		t.Errorf("expected markdown table with 'Model' column header, got: %s", text[:intMin(len(text), 500)])
	}
}

// -----------------------------------------------------------------------
// 15. Discovery endpoint integration tests
// -----------------------------------------------------------------------

// TestIntegrationDiscovery_OpenAPI_Returns200WithCorrectContentType verifies
// that /openapi.json is accessible without auth and has correct content type.
func TestIntegrationDiscovery_OpenAPI_Returns200WithCorrectContentType(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/openapi.json", nil)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
	// Must be valid JSON with openapi version.
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if v, _ := doc["openapi"].(string); !strings.HasPrefix(v, "3.1") {
		t.Errorf("expected openapi 3.1.x, got %q", v)
	}
}

// TestIntegrationDiscovery_AIPlugin_Returns200WithRequiredFields verifies
// that /.well-known/ai-plugin.json is accessible without auth and has required fields.
func TestIntegrationDiscovery_AIPlugin_Returns200WithRequiredFields(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/.well-known/ai-plugin.json", nil)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}

	var manifest struct {
		NameForHuman string `json:"name_for_human"`
		NameForModel string `json:"name_for_model"`
		Description  string `json:"description_for_model"`
		API          struct {
			URL string `json:"url"`
		} `json:"api"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatalf("invalid JSON: %v; body: %s", err, body)
	}
	if manifest.NameForHuman != "LLM Rates" {
		t.Errorf("name_for_human: expected 'LLM Rates', got %q", manifest.NameForHuman)
	}
	if manifest.NameForModel != "llmrates" {
		t.Errorf("name_for_model: expected 'llmrates', got %q", manifest.NameForModel)
	}
	if manifest.Description == "" {
		t.Error("description_for_model must not be empty")
	}
	if manifest.API.URL != "/openapi.json" {
		t.Errorf("api.url: expected '/openapi.json', got %q", manifest.API.URL)
	}
}

// TestIntegrationDiscovery_LLMsTxt_Returns200WithAuthInstructions verifies
// that /llms.txt is accessible without auth and contains auth instructions.
func TestIntegrationDiscovery_LLMsTxt_Returns200WithAuthInstructions(t *testing.T) {
	app, _, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/llms.txt", nil)
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("expected text/plain Content-Type, got %q", ct)
	}

	text := string(body)
	// Must contain auth instructions.
	if !strings.Contains(text, "Authorization") {
		t.Error("llms.txt must contain auth instructions (Authorization header)")
	}
	// Must mention /v1/ask endpoint.
	if !strings.Contains(text, "/v1/ask") {
		t.Error("llms.txt must mention /v1/ask endpoint")
	}
	// Must contain curl example.
	if !strings.Contains(text, "curl") {
		t.Error("llms.txt must contain curl example commands")
	}
}

// TestIntegrationWebhook_RoundTrip verifies the full lifecycle:
// 1. Register a webhook via API
// 2. Reconcile a price change (triggers fan-out)
// 3. Process the asynq task via Worker handler
// 4. Client receives the HTTP POST with correct payload/signature
func TestIntegrationWebhook_RoundTrip(t *testing.T) {
	app, testDB, testRedis := setupTestApp(t)
	ctx := context.Background()

	// Clear redis to avoid picking up stale tasks from previous failed runs
	if err := testRedis.FlushAll(ctx).Err(); err != nil {
		t.Logf("Warning: flush redis failed: %v", err)
	}

	// --- 1. Register Webhook ---
	var gotPayload []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-LLMPricing-Signature")
		gotPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Register with a valid HTTPS placeholder to pass initial validation
	status, body := apiPost(t, app, "/v1/webhooks", proAuth, map[string]string{
		"url": "https://example.com/webhook",
	})
	if status != fiber.StatusCreated {
		t.Fatalf("register webhook failed: %d %s", status, body)
	}
	var whResp struct {
		Data struct {
			ID     string `json:"id"`
			Secret string `json:"secret"`
		} `json:"data"`
	}
	json.Unmarshal(body, &whResp)

	// Manually override the URL in the DB to point to our local HTTP test server
	// (bypassing the production HTTPS-only and SSRF/IP-safety checks).
	_, err := testDB.Exec(ctx, "UPDATE webhooks SET url = $1 WHERE id = $2", srv.URL, whResp.Data.ID)
	if err != nil {
		t.Fatalf("failed to override webhook URL in DB: %v (ID: %q)", err, whResp.Data.ID)
	}

	// --- 2. Trigger Price Change ---
	// We need a Reconciler that points to the same DB/Redis/Asynq
	r := reconciler.New(testDB)
	r.SetRedisClient(testRedis)
	rURL := redisURL()
	// Use the same address for everything to avoid hitting wrong Redis instances
	asynqOpt := asynq.RedisClientOpt{Addr: strings.TrimPrefix(rURL, "redis://")}

	r.SetAsynqClient(asynq.NewClient(asynqOpt))
	r.SetAsynqClient(asynq.NewClient(asynqOpt))

	// Reconcile a single-source change twice to trigger auto-publish
	diffs := []diff.PriceDiff{{
		ModelSlug: "openai/gpt-4o",
		Field:     models.PriceFieldInput,
		NewValue:  0.000009,
		Source:    "openrouter",
	}}

	// Cycle 1: track in pending
	_ = r.Reconcile(ctx, diffs)
	// Cycle 2: publish
	_ = r.Reconcile(ctx, diffs)

	// --- 3. Process Asynq Task ---
	// We don't want to spin up a full worker, just handle the enqueued task manually.
	inspect := asynq.NewInspector(asynqOpt)
	tasks, err := inspect.ListPendingTasks("default")
	if err != nil || len(tasks) == 0 {
		t.Fatalf("expected enqueued webhook task, found 0 (err: %v)", err)
	}

	// Find our task
	var targetTask *asynq.Task
	for _, info := range tasks {
		if info.Type == webhooks.TypeWebhookDeliver {
			targetTask = asynq.NewTask(info.Type, info.Payload)
			break
		}
	}
	if targetTask == nil {
		t.Fatal("webhook:deliver task not found in queue")
	}

	// Handle it using the real worker handler
	wh := worker.NewWebhookDeliveryHandler(stableWebhookKeyHex) // Use same stable key
	if err := wh.Handle(ctx, targetTask); err != nil {
		t.Fatalf("worker handle failed: %v", err)
	}

	// --- 4. Verify Delivery ---
	if len(gotPayload) == 0 {
		t.Fatal("webhook recipient never called")
	}
	if gotSig == "" {
		t.Error("missing X-LLMPricing-Signature header")
	}

	var delivered webhooks.Payload
	if err := json.Unmarshal(gotPayload, &delivered); err != nil {
		t.Fatalf("unmarshal delivered payload: %v", err)
	}
	if delivered.NewPriceInput != 0.000009 {
		t.Errorf("expected new_price_input=0.000009, got %f", delivered.NewPriceInput)
	}

	// Verify HMAC
	mac := hmac.New(sha256.New, []byte(whResp.Data.Secret))
	mac.Write(gotPayload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != expectedSig {
		t.Errorf("HMAC mismatch\n got: %s\n want: %s", gotSig, expectedSig)
	}
}

// intMin returns the smaller of a and b.
// Go 1.24 provides a built-in min() for ordered types, but we name this
// intMin to avoid any conflict with the built-in in the test package scope.
func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
