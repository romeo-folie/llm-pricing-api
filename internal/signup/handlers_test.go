package signup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/signup"
)

// ─── Test doubles ─────────────────────────────────────────────────────────────

type noopMailer struct{ err error }

func (m *noopMailer) SendMagicLink(_ context.Context, _, _ string, _ int) error { return m.err }

type mockIssuer struct {
	createKeyID   string
	createKeyText string
	createErr     error
	revokeErr     error
}

func (m *mockIssuer) CreateKey(_ context.Context, _, _ string) (string, string, error) {
	return m.createKeyID, m.createKeyText, m.createErr
}
func (m *mockIssuer) RevokeKey(_ context.Context, _ string) error { return m.revokeErr }

// noopGuard allows all requests.
type noopGuard struct{}

func (g *noopGuard) CheckRequestLink(_ context.Context, _, _ string) error { return nil }
func (g *noopGuard) CheckRegenerateKey(_ context.Context, _ string) error  { return nil }

// guardInterface provides the same method set as AbuseGuard for mocking.
// We wire a custom handler helper for tests that need abuse control mocking.

// ─── Test helpers ─────────────────────────────────────────────────────────────

func buildApp(store signup.Store, mailer signup.Mailer, issuer signup.KeyIssuer) *fiber.App {
	cfg := &signup.HandlerConfig{
		SigningSecret:     "test-secret-32bytes-padding-here",
		TokenTTL:          15 * time.Minute,
		MagicLinkBase:     "http://localhost",
		MagicLinkPath:     "/signup/verify",
		UnkeyAPIID:        "api_test",
		SessionTTL:        24 * time.Hour,
		SessionCookieName: "llmrates_signup",
		SessionSecure:     false,
		ResendCooldown:    0,
		MaxRequestsPerHour: 0, // disabled in tests
	}
	guard := signup.NewAbuseGuard(nil, cfg) // nil redis → guard fails open
	h := signup.NewHandlers(store, mailer, issuer, guard, cfg, zerolog.Nop())

	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h.Register(app.Group("/auth/signup"))
	return app
}

func doRequest(app *fiber.App, method, url string, body interface{}) *http.Response {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, url, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, _ := app.Test(req, 5000)
	return resp
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestRequestLink_MissingEmail(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/request-link", map[string]string{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRequestLink_InvalidEmail(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/request-link", map[string]string{"email": "notanemail"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRequestLink_Success(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/request-link", map[string]string{"email": "user@example.com"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRequestLink_MailerErrorStillReturns200(t *testing.T) {
	// Mailer failure must not leak to the client.
	app := buildApp(newMock(), &noopMailer{err: errors.New("smtp down")}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/request-link", map[string]string{"email": "user@example.com"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 even on mailer error, got %d", resp.StatusCode)
	}
}

func TestVerify_MissingToken(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "GET", "/auth/signup/verify", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestVerify_InvalidSignature(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "GET", "/auth/signup/verify?token=badtoken", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMe_NoSession(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "GET", "/auth/signup/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestIssueKey_NoSession(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/issue-key", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRegenerateKey_NoSession(t *testing.T) {
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/regenerate-key", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTokenRoundTrip(t *testing.T) {
	cfg := signup.TokenConfig{
		SigningSecret: "test-secret-32bytes-padding-here",
		TTL:           15 * time.Minute,
		BaseURL:       "http://localhost",
		Path:          "/signup/verify",
	}
	_, tokenHash, magicLink, expiresAt, err := signup.GenerateToken(cfg)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if expiresAt.Before(time.Now()) {
		t.Error("token should not be already expired")
	}

	// Extract the signed token from the URL and parse it.
	idx := len("http://localhost/signup/verify?token=")
	signed := magicLink[idx:]
	_, parsedHash, err := signup.ParseToken(signed, cfg.SigningSecret)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if parsedHash != tokenHash {
		t.Errorf("hash mismatch: want %q got %q", tokenHash, parsedHash)
	}
}

func TestTokenParseRejectsBadSignature(t *testing.T) {
	cfg := signup.TokenConfig{
		SigningSecret: "correct-secret-32bytes-paddddddd",
		TTL:           15 * time.Minute,
		BaseURL:       "http://localhost",
		Path:          "/signup/verify",
	}
	_, _, magicLink, _, _ := signup.GenerateToken(cfg)
	idx := len("http://localhost/signup/verify?token=")
	signed := magicLink[idx:]

	// Tamper with the signature
	runes := []rune(signed)
	runes[len(runes)-1] ^= 'x'
	tampered := string(runes)

	_, _, err := signup.ParseToken(tampered, cfg.SigningSecret)
	if err == nil {
		t.Error("expected error for tampered token")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	secret := "session-signing-secret-32bytes!!"
	sess := signup.Session{
		IdentityID: "identity-uuid-123",
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	encoded, err := signup.EncodeSession(sess, secret)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := signup.DecodeSession(encoded, secret)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.IdentityID != sess.IdentityID {
		t.Errorf("identity mismatch: want %q got %q", sess.IdentityID, decoded.IdentityID)
	}
}

func TestSessionDecodeExpired(t *testing.T) {
	secret := "session-signing-secret-32bytes!!"
	sess := signup.Session{
		IdentityID: "identity-uuid-123",
		ExpiresAt:  time.Now().Add(-time.Second), // already expired
	}
	encoded, _ := signup.EncodeSession(sess, secret)
	_, err := signup.DecodeSession(encoded, secret)
	if !errors.Is(err, signup.ErrSessionExpired) {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}
}

func TestSessionDecodeInvalidSignature(t *testing.T) {
	secret := "session-signing-secret-32bytes!!"
	sess := signup.Session{
		IdentityID: "identity-uuid-123",
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	encoded, _ := signup.EncodeSession(sess, secret)
	_, err := signup.DecodeSession(encoded+"tampered", "wrong-secret")
	if !errors.Is(err, signup.ErrSessionInvalid) {
		t.Errorf("expected ErrSessionInvalid, got %v", err)
	}
}
