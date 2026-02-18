package middleware_test

import (
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/middleware"
)

// rateLimitCacheKey mirrors the key pattern used by the RateLimit middleware.
func rateLimitCacheKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	hash := fmt.Sprintf("%x", sum)
	date := time.Now().UTC().Format("2006-01-02")
	return fmt.Sprintf("ratelimit:%s:%s", hash, date)
}

// newRateLimitApp builds a Fiber app that:
//  1. Sets LocalKeyTier and LocalKeyHash from the supplied values.
//  2. Applies the RateLimit middleware.
//  3. Returns 200 on success.
func newRateLimitApp(tier, keyHash string, redisClient *redis.Client) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/v1/test",
		func(c *fiber.Ctx) error {
			c.Locals(middleware.LocalKeyTier, tier)
			c.Locals(middleware.LocalKeyHash, keyHash)
			return c.Next()
		},
		middleware.RateLimit(redisClient),
		func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		},
	)
	return app
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

// TestRateLimit_Pro_Unlimited verifies that Pro tier requests always pass
// without touching Redis.
func TestRateLimit_Pro_Unlimited(t *testing.T) {
	db, mock := redismock.NewClientMock()
	// No expectations — Redis must NOT be touched for Pro.
	app := newRateLimitApp("pro", hashOf("prokey"), db)

	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unexpected Redis calls for Pro tier: %v", err)
	}
}

// TestRateLimit_Free_UnderLimit allows a request when the counter is below 100.
func TestRateLimit_Free_UnderLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("freekey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	// Increment returns 1 (first request of the day).
	mock.ExpectIncr(counterKey).SetVal(1)
	// ExpireAt and Expire are called when count == 1; the middleware ignores
	// errors from these calls, so we don't need to register expectations.
	// The mock will return errors for unexpected calls, but that's fine.

	app := newRateLimitApp("free", hash, db)
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

// TestRateLimit_Free_AtLimit allows a request exactly at the limit (100).
func TestRateLimit_Free_AtLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("freelimitkey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	mock.ExpectIncr(counterKey).SetVal(100)

	app := newRateLimitApp("free", hash, db)
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200 at limit=100, got %d", resp.StatusCode)
	}
}

// TestRateLimit_Free_OverLimit blocks a request when counter exceeds 100.
func TestRateLimit_Free_OverLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("freeoverlimitkey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	mock.ExpectIncr(counterKey).SetVal(101)

	app := newRateLimitApp("free", hash, db)
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("want 429, got %d", resp.StatusCode)
	}
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Error("want Retry-After header, got empty")
	}
}

// TestRateLimit_Developer_UnderLimit allows a request below the 10k limit.
func TestRateLimit_Developer_UnderLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("devkey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	mock.ExpectIncr(counterKey).SetVal(5000)

	app := newRateLimitApp("developer", hash, db)
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

// TestRateLimit_Developer_OverLimit blocks a request above the 10k limit.
func TestRateLimit_Developer_OverLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("devoverlimitkey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	mock.ExpectIncr(counterKey).SetVal(10_001)

	app := newRateLimitApp("developer", hash, db)
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("want 429, got %d", resp.StatusCode)
	}
}

// TestRateLimit_RedisError_AllowsThrough verifies that a Redis failure does
// not block the request (fail-open strategy).
func TestRateLimit_RedisError_AllowsThrough(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("rediserrkey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	mock.ExpectIncr(counterKey).SetErr(fmt.Errorf("redis: connection refused"))

	app := newRateLimitApp("free", hash, db)
	req := httptest.NewRequest("GET", "/v1/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	// On Redis failure, the middleware must not block the request.
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200 (fail-open on Redis error), got %d", resp.StatusCode)
	}
}

// TestRateLimit_429_HasProblemJSON verifies the 429 body is RFC 7807.
func TestRateLimit_429_HasProblemJSON(t *testing.T) {
	db, mock := redismock.NewClientMock()
	hash := hashOf("problemjsonkey")
	date := time.Now().UTC().Format("2006-01-02")
	counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, date)

	mock.ExpectIncr(counterKey).SetVal(999_999)

	app := newRateLimitApp("free", hash, db)
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
