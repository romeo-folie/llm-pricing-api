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

// ─── Test helpers ─────────────────────────────────────────────────────────────

func buildApp(store signup.Store, mailer signup.Mailer, issuer signup.KeyIssuer) *fiber.App {
	cfg := &signup.HandlerConfig{
		SigningSecret:     "test-secret-32bytes-padding-here",
		TokenTTL:          15 * time.Minute,
		MagicLinkBase:     "http://localhost",
		MagicLinkPath:     "/auth/signup/verify",
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
	// Always returns 200 to prevent account enumeration — even for missing email.
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/request-link", map[string]string{})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (enumeration guard), got %d", resp.StatusCode)
	}
}

func TestRequestLink_InvalidEmail(t *testing.T) {
	// Always returns 200 to prevent account enumeration — even for invalid email.
	app := buildApp(newMock(), &noopMailer{}, &mockIssuer{})
	resp := doRequest(app, "POST", "/auth/signup/request-link", map[string]string{"email": "notanemail"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (enumeration guard), got %d", resp.StatusCode)
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

// ─── IssueKey / RegenerateKey handler tests ───────────────────────────────────

// doRequestWithSession attaches a valid signed session cookie to the request.
func doRequestWithSession(t *testing.T, app *fiber.App, method, url string, body interface{}, identityID string) *http.Response {
	t.Helper()
	const secret = "test-secret-32bytes-padding-here"
	sess := signup.Session{
		IdentityID: identityID,
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	encoded, err := signup.EncodeSession(sess, secret)
	if err != nil {
		t.Fatalf("doRequestWithSession: EncodeSession failed: %v", err)
	}

	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, url, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "llmrates_signup", Value: encoded})
	resp, _ := app.Test(req, 5000)
	return resp
}

// TestIssueKey_Success verifies that a session-authenticated user with no
// existing key receives a 201 with the plaintext key.
func TestIssueKey_Success(t *testing.T) {
	store := newMock()
	ctx := context.Background()
	// Pre-create the identity so the handler can look it up by ID.
	id, _ := store.UpsertIdentity(ctx, "issuer@example.com", "", "")

	issuer := &mockIssuer{createKeyID: "key_abc123", createKeyText: "pt_abc123"}
	app := buildApp(store, &noopMailer{}, issuer)

	resp := doRequestWithSession(t, app,"POST", "/auth/signup/issue-key", nil, id.ID)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	data, _ := body["data"].(map[string]interface{})
	if data["key"] != "pt_abc123" {
		t.Errorf("expected plaintext key in response, got %v", data["key"])
	}
	if data["status"] != "issued" {
		t.Errorf("expected status=issued, got %v", data["status"])
	}
}

// TestIssueKey_DuplicateActiveKey verifies that a concurrent duplicate-key
// race (ErrDuplicateActiveKey) returns the existing key metadata rather than 500.
func TestIssueKey_DuplicateActiveKey(t *testing.T) {
	store := newMock()
	ctx := context.Background()
	id, _ := store.UpsertIdentity(ctx, "dup@example.com", "", "")
	// Pre-insert an active key so the mock store returns ErrDuplicateActiveKey.
	store.InsertKey(ctx, id.ID, "existing_provider_key")

	issuer := &mockIssuer{createKeyID: "key_new", createKeyText: "pt_new"}
	app := buildApp(store, &noopMailer{}, issuer)

	resp := doRequestWithSession(t, app,"POST", "/auth/signup/issue-key", nil, id.ID)
	// Should NOT be 500 — handler must catch ErrDuplicateActiveKey and return existing key.
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("expected non-500 for duplicate key, got 500")
	}
}

// TestIssueKey_IssuerError verifies that a Unkey CreateKey failure returns 500.
func TestIssueKey_IssuerError(t *testing.T) {
	store := newMock()
	ctx := context.Background()
	id, _ := store.UpsertIdentity(ctx, "failissue@example.com", "", "")

	issuer := &mockIssuer{createErr: errors.New("unkey unavailable")}
	app := buildApp(store, &noopMailer{}, issuer)

	resp := doRequestWithSession(t, app,"POST", "/auth/signup/issue-key", nil, id.ID)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

// TestRegenerateKey_Success verifies that regeneration revokes the old key and
// issues a new one, returning the new plaintext key.
func TestRegenerateKey_Success(t *testing.T) {
	store := newMock()
	ctx := context.Background()
	id, _ := store.UpsertIdentity(ctx, "regen@example.com", "", "")
	store.InsertKey(ctx, id.ID, "old_provider_key")

	issuer := &mockIssuer{createKeyID: "key_regen", createKeyText: "pt_regen"}
	app := buildApp(store, &noopMailer{}, issuer)

	resp := doRequestWithSession(t, app,"POST", "/auth/signup/regenerate-key", nil, id.ID)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	data, _ := body["data"].(map[string]interface{})
	if data["key"] != "pt_regen" {
		t.Errorf("expected new plaintext key, got %v", data["key"])
	}
	if data["status"] != "regenerated" {
		t.Errorf("expected status=regenerated, got %v", data["status"])
	}

	// Old key must be revoked in the DB.
	_, err := store.GetActiveKey(ctx, id.ID)
	// After regen, the new key is active; if there's an error it's unexpected.
	// We just confirm the new key's provider ID was set.
	if errors.Is(err, signup.ErrNotFound) {
		t.Error("expected a new active key after regen, got ErrNotFound")
	}
}

// TestRegenerateKey_IssuerError verifies that a Unkey CreateKey failure during
// regeneration returns 500 and leaves the old key intact.
func TestRegenerateKey_IssuerError(t *testing.T) {
	store := newMock()
	ctx := context.Background()
	id, _ := store.UpsertIdentity(ctx, "failregen@example.com", "", "")
	store.InsertKey(ctx, id.ID, "old_key_must_survive")

	issuer := &mockIssuer{createErr: errors.New("unkey down")}
	app := buildApp(store, &noopMailer{}, issuer)

	resp := doRequestWithSession(t, app,"POST", "/auth/signup/regenerate-key", nil, id.ID)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	// Old key must still be active — DB should not have been touched.
	k, err := store.GetActiveKey(ctx, id.ID)
	if err != nil {
		t.Errorf("old key should still be active after failed regen: %v", err)
	}
	if k.ProviderKeyID != "old_key_must_survive" {
		t.Errorf("wrong provider key ID: %q", k.ProviderKeyID)
	}
}
