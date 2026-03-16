package mailer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"llm-pricing-api/internal/mailer"
)

func newTestMailer(serverURL string) *mailer.Mailer {
	m := mailer.New("test-key", "noreply@test.com")
	m.SetBaseURL(serverURL)
	return m
}

// TestSendMagicLink_Success verifies a 200 response succeeds with no error.
func TestSendMagicLink_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("want Content-Type=application/json, got %q", ct)
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("want Bearer auth header, got %q", auth)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_123"}`))
	}))
	defer srv.Close()

	m := newTestMailer(srv.URL)
	err := m.SendMagicLink(context.Background(), "user@example.com", "https://example.com/verify?token=abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSendMagicLink_JSONError verifies that a JSON error response is parsed.
func TestSendMagicLink_JSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		resp := map[string]string{"name": "validation_error", "message": "invalid email"}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	m := newTestMailer(srv.URL)
	err := m.SendMagicLink(context.Background(), "bad", "https://example.com/verify?token=abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "validation_error") {
		t.Errorf("error should contain 'validation_error', got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid email") {
		t.Errorf("error should contain 'invalid email', got: %v", err)
	}
}

// TestSendMagicLink_NonJSONError verifies that a non-JSON error body is returned raw.
func TestSendMagicLink_NonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("gateway timeout"))
	}))
	defer srv.Close()

	m := newTestMailer(srv.URL)
	err := m.SendMagicLink(context.Background(), "user@example.com", "https://example.com/verify?token=abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "gateway timeout") {
		t.Errorf("error should contain raw body, got: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code 500, got: %v", err)
	}
}

// TestSendMagicLink_EmptyJSONFields verifies fallback when JSON has empty fields.
func TestSendMagicLink_EmptyJSONFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"name":"","message":""}`))
	}))
	defer srv.Close()

	m := newTestMailer(srv.URL)
	err := m.SendMagicLink(context.Background(), "user@example.com", "https://example.com/verify?token=abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// With empty name and message, it should fall through to the raw body path.
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

// TestSendMagicLink_ContextCancelled verifies cancellation is propagated.
func TestSendMagicLink_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := newTestMailer(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.
	err := m.SendMagicLink(ctx, "user@example.com", "https://example.com/verify?token=abc")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
