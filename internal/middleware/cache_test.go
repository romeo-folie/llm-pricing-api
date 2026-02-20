package middleware_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/middleware"
)

// TestCacheNonGETPassThrough verifies that non-GET requests are not cached
// and receive Cache-Control: no-store.
func TestCacheNonGETPassThrough(t *testing.T) {
	rc, _ := redismock.NewClientMock()
	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Post("/v1/models", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("POST", "/v1/models", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control: got %q, want %q", cc, "no-store")
	}
}

// TestCacheUncachedRoute verifies that routes not in the TTL map receive
// Cache-Control: no-store even for GET requests.
func TestCacheUncachedRoute(t *testing.T) {
	rc, _ := redismock.NewClientMock()
	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Get("/v1/webhooks", func(c *fiber.Ctx) error {
		return c.SendString("webhook")
	})

	req := httptest.NewRequest("GET", "/v1/webhooks", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("Cache-Control: got %q, want %q", cc, "no-store")
	}
}

// TestCacheCacheableRoute verifies that cacheable routes receive a
// Cache-Control: max-age=N, public header on a cache miss.
func TestCacheCacheableRoute(t *testing.T) {
	rc, mock := redismock.NewClientMock()
	// Simulate cache miss for the GET /v1/models request.
	mock.Regexp().ExpectGet(`cache:GET:/v1/models:.*`).RedisNil()

	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Get("/v1/models", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": "test"})
	})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	// Should be max-age=300, public (5 minutes = 300 seconds).
	if !strings.HasPrefix(cc, "max-age=") {
		t.Errorf("Cache-Control: got %q, want prefix max-age=", cc)
	}
	if !strings.Contains(cc, "public") {
		t.Errorf("Cache-Control: got %q, want it to contain 'public'", cc)
	}
}

// TestCacheModelsIDRoute verifies that /v1/models/:id prefix matches correctly.
func TestCacheModelsIDRoute(t *testing.T) {
	rc, mock := redismock.NewClientMock()
	mock.Regexp().ExpectGet(`cache:GET:/v1/models/gpt-4:.*`).RedisNil()

	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Get("/v1/models/:id", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"id": c.Params("id")})
	})

	req := httptest.NewRequest("GET", "/v1/models/gpt-4", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if !strings.HasPrefix(cc, "max-age=") {
		t.Errorf("Cache-Control for /v1/models/:id: got %q, want max-age prefix", cc)
	}
}

// TestCacheContextRoute verifies that /v1/context gets a 1-hour TTL (3600s).
func TestCacheContextRoute(t *testing.T) {
	rc, mock := redismock.NewClientMock()
	mock.Regexp().ExpectGet(`cache:GET:/v1/context:.*`).RedisNil()

	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Get("/v1/context", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"context": "snapshot"})
	})

	req := httptest.NewRequest("GET", "/v1/context", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "max-age=3600") {
		t.Errorf("Cache-Control for /v1/context: got %q, want max-age=3600", cc)
	}
}

// TestCacheCompareRoute verifies that /v1/compare gets a 10-minute TTL (600s).
func TestCacheCompareRoute(t *testing.T) {
	rc, mock := redismock.NewClientMock()
	mock.Regexp().ExpectGet(`cache:GET:/v1/compare:.*`).RedisNil()

	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Get("/v1/compare", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"compare": "result"})
	})

	req := httptest.NewRequest("GET", "/v1/compare?models=gpt-4,claude", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "max-age=600") {
		t.Errorf("Cache-Control for /v1/compare: got %q, want max-age=600", cc)
	}
}

// TestCacheHitBypassesRateLimit verifies that a Redis cache hit short-circuits
// the middleware chain before RateLimit is reached. The INCR expectation is
// deliberately set to an over-limit value (101) so that if RateLimit were
// reached the response would be 429 — making a wrong ordering observable.
func TestCacheHitBypassesRateLimit(t *testing.T) {
	rc, mock := redismock.NewClientMock()

	hash := "bypasstesthash"
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	cachedBody := []byte(`{"data":"cached"}`)
	mock.Regexp().ExpectGet(`cache:GET:/v1/models:.*`).SetVal(string(cachedBody))
	// If RateLimit is reached, INCR would return 101 (over limit) → 429.
	mock.ExpectIncr(counterKey).SetVal(101)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalKeyTier, "free")
		c.Locals(middleware.LocalKeyHash, hash)
		return c.Next()
	})
	app.Use(middleware.Cache(rc))
	app.Use(middleware.RateLimit(rc))
	app.Get("/v1/models", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": "fresh"})
	})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	// 200 = cache hit short-circuited before RateLimit. 429 = ordering is wrong.
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("cache hit must bypass rate limit: got %d, want 200", resp.StatusCode)
	}
}

// TestCacheHit verifies that when a cache hit occurs the cached body is
// returned with the correct headers and the handler is not called.
func TestCacheHit(t *testing.T) {
	rc, mock := redismock.NewClientMock()
	cachedBody := []byte(`{"data":"cached"}`)
	mock.Regexp().ExpectGet(`cache:GET:/v1/models:.*`).SetVal(string(cachedBody))

	handlerCalled := false
	app := fiber.New()
	app.Use(middleware.Cache(rc))
	app.Get("/v1/models", func(c *fiber.Ctx) error {
		handlerCalled = true
		return c.JSON(fiber.Map{"data": "fresh"})
	})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if handlerCalled {
		t.Error("handler was called on cache hit — expected bypass")
	}
	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "max-age=") || !strings.Contains(cc, "public") {
		t.Errorf("Cache-Control on hit: got %q, want max-age + public", cc)
	}
}
