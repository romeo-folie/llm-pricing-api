package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/middleware"
)

func TestRealIP_NoTrustedProxies_ReturnsCIP(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = middleware.RealIP(c) // no trustedProxies → safe default
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	// XFF is ignored; Fiber test mode reports c.IP() as "0.0.0.0".
	if got != "0.0.0.0" {
		t.Errorf("want 0.0.0.0 (c.IP()), got %q", got)
	}
}

func TestRealIP_TrustedPeer_SingleXFF(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		// Fiber test mode: c.IP() = "0.0.0.0"; trust it as a proxy.
		got = middleware.RealIP(c, "0.0.0.0")
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

func TestRealIP_TrustedPeer_MultiHopXFF_UsesLeftmost(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = middleware.RealIP(c, "0.0.0.0")
		return c.SendStatus(fiber.StatusOK)
	})

	// Leftmost entry is the original client IP; rightmost is the last proxy hop.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42, 10.0.0.1")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	if got != "203.0.113.42" {
		t.Errorf("want leftmost IP 203.0.113.42, got %q", got)
	}
}

func TestRealIP_UntrustedPeer_IgnoresXFF(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		// Trust only 10.0.0.1; Fiber test mode peer is 0.0.0.0 → untrusted.
		got = middleware.RealIP(c, "10.0.0.1")
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	// Peer is not trusted, so XFF is ignored.
	if got != "0.0.0.0" {
		t.Errorf("want 0.0.0.0 (untrusted peer), got %q", got)
	}
}

func TestRealIP_TrustedPeer_NoXFF_FallsBackToIP(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		got = middleware.RealIP(c, "0.0.0.0")
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatal(err)
	}
	if got != "0.0.0.0" {
		t.Errorf("want 0.0.0.0 fallback, got %q", got)
	}
}
