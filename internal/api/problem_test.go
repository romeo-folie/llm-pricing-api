package api_test

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

// TestProblemDetailConstructors verifies that each constructor sets the
// correct HTTP status code, type URI, title, and detail.
func TestProblemDetailConstructors(t *testing.T) {
	tests := []struct {
		name           string
		fn             func(string) *api.ProblemDetail
		wantStatus     int
		wantTypeContains string
		wantTitle      string
	}{
		{
			name:             "NewUnauthorized",
			fn:               api.NewUnauthorized,
			wantStatus:       fiber.StatusUnauthorized,
			wantTypeContains: "unauthorized",
			wantTitle:        "Unauthorized",
		},
		{
			name:             "NewForbidden",
			fn:               api.NewForbidden,
			wantStatus:       fiber.StatusForbidden,
			wantTypeContains: "forbidden",
			wantTitle:        "Forbidden",
		},
		{
			name:             "NewNotFound",
			fn:               api.NewNotFound,
			wantStatus:       fiber.StatusNotFound,
			wantTypeContains: "not-found",
			wantTitle:        "Not Found",
		},
		{
			name:             "NewBadRequest",
			fn:               api.NewBadRequest,
			wantStatus:       fiber.StatusBadRequest,
			wantTypeContains: "bad-request",
			wantTitle:        "Bad Request",
		},
		{
			name:             "NewTooManyRequests",
			fn:               api.NewTooManyRequests,
			wantStatus:       fiber.StatusTooManyRequests,
			wantTypeContains: "too-many-requests",
			wantTitle:        "Too Many Requests",
		},
		{
			name:             "NewInternalError",
			fn:               api.NewInternalError,
			wantStatus:       fiber.StatusInternalServerError,
			wantTypeContains: "internal-server-error",
			wantTitle:        "Internal Server Error",
		},
	}

	const detail = "test detail message"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := tt.fn(detail)

			if pd.Status != tt.wantStatus {
				t.Errorf("Status: got %d, want %d", pd.Status, tt.wantStatus)
			}
			if !contains(pd.Type, tt.wantTypeContains) {
				t.Errorf("Type: got %q, want it to contain %q", pd.Type, tt.wantTypeContains)
			}
			if pd.Title != tt.wantTitle {
				t.Errorf("Title: got %q, want %q", pd.Title, tt.wantTitle)
			}
			if pd.Detail != detail {
				t.Errorf("Detail: got %q, want %q", pd.Detail, detail)
			}
			// Type must be an absolute URI.
			if !startsWith(pd.Type, "https://") {
				t.Errorf("Type must be an absolute https URI, got %q", pd.Type)
			}
		})
	}
}

// TestProblemDetailError verifies that ProblemDetail implements the error interface.
func TestProblemDetailError(t *testing.T) {
	pd := api.NewNotFound("resource not found")
	var err error = pd // compile-time interface check
	if err.Error() != "resource not found" {
		t.Errorf("Error(): got %q, want %q", err.Error(), "resource not found")
	}
}

// TestErrorHandlerWithProblemDetail verifies that the Fiber ErrorHandler
// renders a ProblemDetail error with application/problem+json content type.
func TestErrorHandlerWithProblemDetail(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	app.Get("/test", func(c *fiber.Ctx) error {
		return api.NewNotFound("item not found")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("Status: got %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
	ct := resp.Header.Get("Content-Type")
	if !contains(ct, "application/problem+json") {
		t.Errorf("Content-Type: got %q, want it to contain application/problem+json", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	var pd api.ProblemDetail
	if err := json.Unmarshal(body, &pd); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if pd.Status != fiber.StatusNotFound {
		t.Errorf("body.status: got %d, want %d", pd.Status, fiber.StatusNotFound)
	}
	if pd.Detail != "item not found" {
		t.Errorf("body.detail: got %q, want %q", pd.Detail, "item not found")
	}
}

// TestErrorHandlerWithFiberError verifies that native Fiber errors are
// converted to ProblemDetail responses.
func TestErrorHandlerWithFiberError(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	app.Get("/test", func(c *fiber.Ctx) error {
		return fiber.ErrUnauthorized
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("Status: got %d, want %d", resp.StatusCode, fiber.StatusUnauthorized)
	}
	ct := resp.Header.Get("Content-Type")
	if !contains(ct, "application/problem+json") {
		t.Errorf("Content-Type: got %q, want application/problem+json", ct)
	}
}

// TestErrorHandlerWithGenericError verifies that unknown errors become 500s.
func TestErrorHandlerWithGenericError(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	app.Get("/test", func(c *fiber.Ctx) error {
		return io.EOF // arbitrary non-Fiber error
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("Status: got %d, want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}
}

// TestErrorHandlerInstanceIsPath verifies that Instance is set to the request path.
func TestErrorHandlerInstanceIsPath(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.ErrorHandler,
	})
	app.Get("/models/123", func(c *fiber.Ctx) error {
		return api.NewNotFound("model not found")
	})

	req := httptest.NewRequest("GET", "/models/123", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var pd api.ProblemDetail
	if err := json.Unmarshal(body, &pd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pd.Instance != "/models/123" {
		t.Errorf("Instance: got %q, want %q", pd.Instance, "/models/123")
	}
}

// helpers

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
