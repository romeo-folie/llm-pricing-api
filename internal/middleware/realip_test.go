package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/middleware"
)

func TestRealIP_SingleXFF(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = middleware.RealIP(c)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	if got != "203.0.113.42" {
		t.Errorf("want 203.0.113.42, got %q", got)
	}
}

func TestRealIP_MultiHopXFF_UsesRightmost(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = middleware.RealIP(c)
		return c.SendStatus(fiber.StatusOK)
	})

	// Client spoofs "1.2.3.4" at start; proxy appends the real client IP.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.42")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	if got != "203.0.113.42" {
		t.Errorf("want rightmost IP 203.0.113.42, got %q", got)
	}
}

func TestRealIP_NoXFF_FallsBackToIP(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = middleware.RealIP(c)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	// Fiber test mode reports c.IP() as "0.0.0.0".
	if got != "0.0.0.0" {
		t.Errorf("want 0.0.0.0 fallback, got %q", got)
	}
}
