package middleware_test

import (
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/middleware"
)

// ipWindowKey returns the expected Redis key for the given IP and time.
func ipWindowKey(ip string, t time.Time) string {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(ip)))
	windowSec := int64((15 * time.Minute).Seconds())
	windowIndex := t.Unix() / windowSec
	return fmt.Sprintf("iprl:%s:%d", hash[:16], windowIndex)
}

// fixedTime is a timestamp safely in the middle of a 15-minute window,
// avoiding flakiness from boundary rollovers.
var fixedTime = time.Date(2025, 6, 15, 12, 7, 30, 0, time.UTC)

func fixedClock() time.Time { return fixedTime }

// fixedWindowEnd returns the end of the 15-minute window containing fixedTime.
func fixedWindowEnd() time.Time {
	windowSec := int64((15 * time.Minute).Seconds())
	windowIndex := fixedTime.Unix() / windowSec
	return time.Unix((windowIndex+1)*windowSec, 0)
}

// rateLimitCfg returns an IPRateLimitConfig with the fixed clock.
func rateLimitCfg() middleware.IPRateLimitConfig {
	return middleware.IPRateLimitConfig{TimeNow: fixedClock}
}

// TestIPRateLimit_UnderLimit verifies that requests under the limit pass.
func TestIPRateLimit_UnderLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	key := ipWindowKey("0.0.0.0", fixedTime)

	mock.ExpectIncr(key).SetVal(1)
	mock.ExpectExpireAt(key, fixedWindowEnd()).SetVal(true)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimitWithConfig(db, zerolog.Nop(), rateLimitCfg()), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
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
	key := ipWindowKey("0.0.0.0", fixedTime)

	mock.ExpectIncr(key).SetVal(10)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimitWithConfig(db, zerolog.Nop(), rateLimitCfg()), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
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
	key := ipWindowKey("0.0.0.0", fixedTime)

	mock.ExpectIncr(key).SetVal(11)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimitWithConfig(db, zerolog.Nop(), rateLimitCfg()), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("want 429, got %d", resp.StatusCode)
	}
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Error("want Retry-After header, got empty")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled redis expectations: %v", err)
	}
}

// TestIPRateLimit_RedisError_AllowsThrough verifies fail-open on Redis errors.
func TestIPRateLimit_RedisError_AllowsThrough(t *testing.T) {
	db, mock := redismock.NewClientMock()
	key := ipWindowKey("0.0.0.0", fixedTime)

	mock.ExpectIncr(key).SetErr(fmt.Errorf("redis: connection refused"))

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimitWithConfig(db, zerolog.Nop(), rateLimitCfg()), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
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
	// Fiber test mode reports c.IP() as "0.0.0.0".
	key := ipWindowKey("0.0.0.0", fixedTime)

	mock.MatchExpectationsInOrder(false)
	mock.ExpectIncr(key).SetVal(11)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	app.Get("/test", middleware.IPRateLimitWithConfig(db, zerolog.Nop(), rateLimitCfg()), func(c *fiber.Ctx) error {
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
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled redis expectations: %v", err)
	}
}

// TestIPRateLimit_XForwardedFor verifies that when trustedProxies are provided,
// the XFF header is honoured for IP hashing (leftmost entry = client IP).
func TestIPRateLimit_XForwardedFor(t *testing.T) {
	db, mock := redismock.NewClientMock()
	clientIP := "203.0.113.42"
	key := ipWindowKey(clientIP, fixedTime)

	mock.ExpectIncr(key).SetVal(11)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler:          api.ErrorHandler,
	})
	// Pass "0.0.0.0" as trusted proxy (Fiber test mode peer IP) so RealIP reads XFF.
	app.Get("/test", middleware.IPRateLimitWithConfig(db, zerolog.Nop(), rateLimitCfg(), "0.0.0.0"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", clientIP)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("want 429 for X-Forwarded-For IP, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled redis expectations: %v", err)
	}
}
