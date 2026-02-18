package api_test

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/models"
)

func TestOK_ReturnsEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		meta := api.TrustMeta{
			Confidence: models.ConfidenceHigh,
			Source:     "openrouter",
			ConfirmedAt: time.Now().UTC(),
		}
		return api.OK(c, fiber.Map{"key": "value"}, meta)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("Status: got %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var env api.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.Source != "openrouter" {
		t.Errorf("meta.source: got %q, want %q", env.Meta.Source, "openrouter")
	}
}

func TestCreated_Returns201(t *testing.T) {
	app := fiber.New()
	app.Post("/test", func(c *fiber.Ctx) error {
		meta := api.TrustMeta{
			Confidence: models.ConfidenceMedium,
			Source:     "litellm",
		}
		return api.Created(c, fiber.Map{"id": 42}, meta)
	})

	req := httptest.NewRequest("POST", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusCreated {
		t.Errorf("Status: got %d, want 201", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var env api.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.Source != "litellm" {
		t.Errorf("meta.source: got %q, want %q", env.Meta.Source, "litellm")
	}
}
