package middleware_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/go-redis/redismock/v9"
	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/middleware"
)

// --- mock verifier ---

type mockVerifier struct {
	valid bool
	tier  string
	err   error
}

func (m *mockVerifier) VerifyKey(_ context.Context, _, _ string) (bool, string, error) {
	return m.valid, m.tier, m.err
}

// --- helpers ---

// cacheKey returns the Redis cache key for rawKey, mirroring the middleware's logic.
func cacheKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "unkey:" + fmt.Sprintf("%x", sum)
}

// doRequest executes a GET /v1/test against app with the supplied Authorization
// header, returning the HTTP status code and the JSON-decoded body map.
func doRequest(app *fiber.App, authHeader string) (int, map[string]interface{}) {
	req := httptest.NewRequest("GET", "/v1/test", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := app.Test(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(body, &m)
	return resp.StatusCode, m
}

func newTestApp(mw fiber.Handler) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/v1/test", mw, func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"tier": c.Locals(middleware.LocalKeyTier),
		})
	})
	return app
}

// --- Auth middleware tests ---

func TestAuth_MissingHeader(t *testing.T) {
	db, _ := redismock.NewClientMock()
	v := &mockVerifier{}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "")

	if status != fiber.StatusUnauthorized {
		t.Errorf("want 401, got %d", status)
	}
	if typ, ok := body["type"].(string); !ok || typ == "" {
		t.Error("expected non-empty 'type' field in RFC 7807 body")
	}
}

func TestAuth_MalformedHeader_NotBearer(t *testing.T) {
	db, _ := redismock.NewClientMock()
	v := &mockVerifier{}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "Basic dXNlcjpwYXNz")

	if status != fiber.StatusUnauthorized {
		t.Errorf("want 401, got %d", status)
	}
	if _, ok := body["status"]; !ok {
		t.Error("expected RFC 7807 'status' field")
	}
}

func TestAuth_EmptyBearerToken(t *testing.T) {
	db, _ := redismock.NewClientMock()
	v := &mockVerifier{}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, _ := doRequest(app, "Bearer ")

	if status != fiber.StatusUnauthorized {
		t.Errorf("want 401, got %d", status)
	}
}

func TestAuth_ValidFreeKey_CacheMiss(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "validfreekey"
	key := cacheKey(rawKey)

	mock.ExpectGet(key).RedisNil()
	// The middleware calls Set after a miss; we don't need to assert the exact
	// value — the mock returns an error for unexpected commands, but the
	// middleware ignores Set errors so the test is not affected.

	v := &mockVerifier{valid: true, tier: "free"}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusOK {
		t.Errorf("want 200, got %d", status)
	}
	if body["tier"] != "free" {
		t.Errorf("want tier='free', got %v", body["tier"])
	}
}

func TestAuth_ValidDeveloperKey_CacheMiss(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "validdevkey"
	key := cacheKey(rawKey)

	mock.ExpectGet(key).RedisNil()

	v := &mockVerifier{valid: true, tier: "developer"}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusOK {
		t.Errorf("want 200, got %d", status)
	}
	if body["tier"] != "developer" {
		t.Errorf("want tier='developer', got %v", body["tier"])
	}
}

func TestAuth_ValidProKey_CacheMiss(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "validprokey"
	key := cacheKey(rawKey)

	mock.ExpectGet(key).RedisNil()

	v := &mockVerifier{valid: true, tier: "pro"}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusOK {
		t.Errorf("want 200, got %d", status)
	}
	if body["tier"] != "pro" {
		t.Errorf("want tier='pro', got %v", body["tier"])
	}
}

func TestAuth_InvalidKey_VerifierReturnsFalse(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "invalidkey"
	key := cacheKey(rawKey)

	mock.ExpectGet(key).RedisNil()

	v := &mockVerifier{valid: false, tier: ""}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusUnauthorized {
		t.Errorf("want 401, got %d", status)
	}
	if body["title"] != "Unauthorized" {
		t.Errorf("want title='Unauthorized', got %v", body["title"])
	}
}

func TestAuth_InvalidKey_VerifierReturnsError(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "errorkey"
	key := cacheKey(rawKey)

	mock.ExpectGet(key).RedisNil()
	// On verifier error, the middleware returns 401 without caching.

	v := &mockVerifier{err: errors.New("network timeout")}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, _ := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusUnauthorized {
		t.Errorf("want 401, got %d", status)
	}
}

func TestAuth_CacheHit_ValidKey(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "cachedvalidkey"
	key := cacheKey(rawKey)

	// Return cached valid result — verifier must NOT be called.
	cachedJSON := `{"valid":true,"tier":"free"}`
	mock.ExpectGet(key).SetVal(cachedJSON)

	// An error verifier ensures the Unkey API is not called on a cache hit.
	v := &mockVerifier{err: errors.New("verifier must not be called on cache hit")}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, body := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusOK {
		t.Errorf("want 200, got %d; body: %v", status, body)
	}
	if body["tier"] != "free" {
		t.Errorf("want tier='free', got %v", body["tier"])
	}
}

func TestAuth_CacheHit_InvalidKey(t *testing.T) {
	db, mock := redismock.NewClientMock()
	const rawKey = "cachedinvalidkey"
	key := cacheKey(rawKey)

	// Return cached invalid result.
	cachedJSON := `{"valid":false,"tier":""}`
	mock.ExpectGet(key).SetVal(cachedJSON)

	v := &mockVerifier{err: errors.New("verifier must not be called on cache hit")}
	app := newTestApp(middleware.Auth(v, db, "api_123"))

	status, _ := doRequest(app, "Bearer "+rawKey)

	if status != fiber.StatusUnauthorized {
		t.Errorf("want 401, got %d", status)
	}
}

func TestAuth_ContentType_IsProblemJSON(t *testing.T) {
	db, _ := redismock.NewClientMock()
	v := &mockVerifier{}
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/v1/test", middleware.Auth(v, db, "api_123"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("want Content-Type=application/problem+json, got %q", ct)
	}
}

// --- RequireTier tests ---

func newTierApp(tier string, minTier string) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/v1/test",
		func(c *fiber.Ctx) error {
			c.Locals(middleware.LocalKeyTier, tier)
			return c.Next()
		},
		middleware.RequireTier(minTier),
		func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		},
	)
	return app
}

func TestRequireTier_Free_NeedsFree(t *testing.T) {
	app := newTierApp("free", "free")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestRequireTier_Free_NeedsDeveloper(t *testing.T) {
	app := newTierApp("free", "developer")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("want 403, got %d", resp.StatusCode)
	}
}

func TestRequireTier_Developer_NeedsDeveloper(t *testing.T) {
	app := newTierApp("developer", "developer")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestRequireTier_Developer_NeedsPro(t *testing.T) {
	app := newTierApp("developer", "pro")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("want 403, got %d", resp.StatusCode)
	}
}

func TestRequireTier_Pro_NeedsPro(t *testing.T) {
	app := newTierApp("pro", "pro")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestRequireTier_Pro_NeedsFree(t *testing.T) {
	app := newTierApp("pro", "free")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestRequireTier_403_HasTierRequiredField(t *testing.T) {
	app := newTierApp("free", "pro")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	var m map[string]interface{}
	_ = json.Unmarshal(rawBody, &m)

	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("want 403, got %d", resp.StatusCode)
	}
	// tier_required is nested inside the RFC 7807 extensions object.
	ext, _ := m["extensions"].(map[string]interface{})
	if ext == nil || ext["tier_required"] != "pro" {
		t.Errorf("want extensions.tier_required='pro', got extensions=%v", ext)
	}
}

func TestRequireTier_403_ContentType_IsProblemJSON(t *testing.T) {
	app := newTierApp("free", "developer")
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("want Content-Type=application/problem+json, got %q", ct)
	}
}
