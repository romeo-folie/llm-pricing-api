package middleware_test

import (
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/middleware"
)

// ipWindowKey returns the expected Redis key for the given IP and current time.
func ipWindowKey(ip string, now time.Time) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(ip)))
	windowSec := int64((15 * time.Minute).Seconds())
	windowIndex := now.Unix() / windowSec
	return fmt.Sprintf("iprl:%s:%d", hash[:16], windowIndex)
}

// TestIPRateLimit_UnderLimit verifies that requests under the limit pass.
func TestIPRateLimit_UnderLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	now := time.Now()
	key := ipWindowKey("0.0.0.0", now)

	mock.ExpectIncr(key).SetVal(1)
	mock.ExpectExpire(key, 15*time.Minute).SetVal(true)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimit(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled redis expectations: %v", err)
	}
}

// TestIPRateLimit_AtLimit verifies that the 10th request still passes.
func TestIPRateLimit_AtLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	now := time.Now()
	key := ipWindowKey("0.0.0.0", now)

	mock.ExpectIncr(key).SetVal(10)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimit(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200 at limit=10, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled redis expectations: %v", err)
	}
}

// TestIPRateLimit_OverLimit verifies that the 11th request is blocked with 429.
func TestIPRateLimit_OverLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	now := time.Now()
	key := ipWindowKey("0.0.0.0", now)

	mock.ExpectIncr(key).SetVal(11)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimit(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
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

// TestIPRateLimit_RedisError_AllowsThrough verifies fail-open on Redis errors.
func TestIPRateLimit_RedisError_AllowsThrough(t *testing.T) {
	db, mock := redismock.NewClientMock()
	now := time.Now()
	key := ipWindowKey("0.0.0.0", now)

	mock.ExpectIncr(key).SetErr(fmt.Errorf("redis: connection refused"))

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimit(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("want 200 (fail-open), got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled redis expectations: %v", err)
	}
}

// TestIPRateLimit_429_HasProblemJSON verifies RFC 7807 content type on 429.
func TestIPRateLimit_429_HasProblemJSON(t *testing.T) {
	db, mock := redismock.NewClientMock()
	now := time.Now()
	// Fiber test mode reports c.IP() as "0.0.0.0".
	key := ipWindowKey("0.0.0.0", now)

	mock.MatchExpectationsInOrder(false)
	mock.ExpectIncr(key).SetVal(11)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimit(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("want 429, got %d (mock expectations: %v)", resp.StatusCode, mock.ExpectationsWereMet())
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("want Content-Type=application/problem+json, got %q", ct)
	}
}

// TestIPRateLimit_XForwardedFor uses the X-Forwarded-For header for IP hashing.
func TestIPRateLimit_XForwardedFor(t *testing.T) {
	db, mock := redismock.NewClientMock()
	clientIP := "203.0.113.42"
	now := time.Now()
	key := ipWindowKey(clientIP, now)

	mock.ExpectIncr(key).SetVal(11)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimit(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", clientIP)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("want 429 for X-Forwarded-For IP, got %d", resp.StatusCode)
	}
}
