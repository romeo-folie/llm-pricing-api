package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/auth"
	"llm-pricing-api/internal/signup"
)

// ── Mock store ──────────────────────────────────────────────────────────────────

// mockStore implements auth.Store with an in-memory backend.
type mockStore struct {
	mu         sync.Mutex
	identities map[string]*signup.Identity // keyed by email
	tokens     map[string]mockToken        // keyed by rawToken
}

type mockToken struct {
	id         string
	identityID string
	expiresAt  time.Time
	usedAt     *time.Time
	createdAt  time.Time
}

func newMockStore() *mockStore {
	return &mockStore{
		identities: make(map[string]*signup.Identity),
		tokens:     make(map[string]mockToken),
	}
}

func (m *mockStore) CreateIdentity(_ context.Context, email, _, _ string) (signup.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.identities[email]; ok {
		return *existing, nil
	}
	now := time.Now()
	id := &signup.Identity{
		ID:        fmt.Sprintf("id-%s", email),
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.identities[email] = id
	return *id, nil
}

func (m *mockStore) FindIdentityByID(_ context.Context, id string) (signup.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ident := range m.identities {
		if ident.ID == id {
			return *ident, nil
		}
	}
	return signup.Identity{}, signup.ErrNotFound
}

func (m *mockStore) MarkEmailVerified(_ context.Context, identityID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ident := range m.identities {
		if ident.ID == identityID {
			now := time.Now()
			ident.EmailVerifiedAt = &now
			return nil
		}
	}
	return signup.ErrNotFound
}

func (m *mockStore) CreateToken(_ context.Context, identityID, rawToken string, expiresAt time.Time) (signup.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[rawToken] = mockToken{
		id:         "tok-" + rawToken,
		identityID: identityID,
		expiresAt:  expiresAt,
		createdAt:  time.Now(),
	}
	return signup.Token{}, nil
}

func (m *mockStore) ConsumeToken(_ context.Context, rawToken string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, ok := m.tokens[rawToken]
	if !ok {
		return "", signup.ErrNotFound
	}
	if tok.usedAt != nil {
		return "", signup.ErrTokenUsed
	}
	if tok.expiresAt.Before(time.Now()) {
		return "", signup.ErrTokenExpired
	}
	now := time.Now()
	tok.usedAt = &now
	m.tokens[rawToken] = tok
	return tok.identityID, nil
}

func (m *mockStore) CountRecentTokens(_ context.Context, identityID string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, tok := range m.tokens {
		if tok.identityID == identityID && !tok.createdAt.Before(since) {
			count++
		}
	}
	return count, nil
}

// ── Mock mailer ─────────────────────────────────────────────────────────────────

type mockMailer struct {
	mu        sync.Mutex
	sendCalls []string
	sendErr   error
}

func (m *mockMailer) SendMagicLink(_ context.Context, email, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sendCalls = append(m.sendCalls, email)
	return nil
}

// ── Test helpers ────────────────────────────────────────────────────────────────

var testCfg = auth.Config{
	SigningSecret:           "test-secret-at-least-32-bytes!!!", // 32 bytes
	MagicLinkTTLMinutes:     15,
	MagicLinkBaseURL:        "https://example.com",
	MagicLinkPath:           "/signup/verify",
	SignupSessionCookieName: "test_session",
	SignupSessionTTLHours:   24,
	SignupSessionSecure:     false,
}

func newTestApp(store auth.Store, mailer auth.Mailer) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}})
	log := zerolog.Nop()
	h := auth.New(store, mailer, testCfg, log)
	authGroup := app.Group("/auth")
	auth.Register(authGroup, h)
	return app
}

func doRequest(t *testing.T, app *fiber.App, method, path, body string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test(%s %s) failed: %v", method, path, err)
	}
	return resp
}

func bodyJSON(resp *http.Response) map[string]any {
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m
}

// ── Tests ───────────────────────────────────────────────────────────────────────

func TestRequestLink_ValidEmail_Returns200(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"alice@example.com"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if _, ok := b["message"]; !ok {
		t.Error("expected generic 'message' key in response")
	}
}

func TestRequestLink_InvalidEmail_Returns400(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"notanemail"}`)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if errMsg, ok := b["error"]; !ok || errMsg == "" {
		t.Error("expected non-empty 'error' field in response body")
	}
}

func TestRequestLink_RateLimited_Returns200(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)

	// Send 3 requests to hit the rate limit.
	for i := 0; i < 3; i++ {
		resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"ratelimit@example.com"}`)
		if resp.StatusCode != 200 {
			t.Fatalf("request %d: status = %d, want 200", i+1, resp.StatusCode)
		}
	}

	// 4th request should still return 200 (no enumeration leak) but not create a new token.
	tokenCountBefore := len(store.tokens)
	resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"ratelimit@example.com"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("rate-limited request: status = %d, want 200", resp.StatusCode)
	}
	tokenCountAfter := len(store.tokens)
	if tokenCountAfter != tokenCountBefore {
		t.Errorf("expected no new tokens after rate limit, got %d -> %d", tokenCountBefore, tokenCountAfter)
	}
}

func TestVerify_ValidToken_Returns200AndSetsCookie(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)

	// Seed identity + token directly.
	store.identities["bob@example.com"] = &signup.Identity{ID: "id-bob", Email: "bob@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens["validtoken123"] = mockToken{id: "tok-1", identityID: "id-bob", expiresAt: time.Now().Add(15 * time.Minute), createdAt: time.Now()}

	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=validtoken123", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if b["verified"] != true {
		t.Error("expected verified=true")
	}
	// Check cookie is set.
	var cookieFound bool
	for _, c := range resp.Cookies() {
		if c.Name == testCfg.SignupSessionCookieName {
			cookieFound = true
			if !c.HttpOnly {
				t.Error("session cookie must be HttpOnly")
			}
		}
	}
	if !cookieFound {
		t.Error("session cookie not set on verify response")
	}
}

func TestVerify_MissingToken_Returns400(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	resp := doRequest(t, app, "GET", "/auth/signup/verify", "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestVerify_InvalidToken_Returns401(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=doesnotexist", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestVerify_ExpiredToken_Returns410(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	store.identities["carol@example.com"] = &signup.Identity{ID: "id-carol", Email: "carol@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens["expiredtok"] = mockToken{id: "tok-exp", identityID: "id-carol", expiresAt: time.Now().Add(-1 * time.Minute), createdAt: time.Now()}
	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=expiredtok", "")
	if resp.StatusCode != 410 {
		t.Fatalf("status = %d, want 410", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if b["error"] != "token expired" {
		t.Errorf("error = %v, want 'token expired'", b["error"])
	}
}

func TestVerify_ReusedToken_Returns410(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	store.identities["dave@example.com"] = &signup.Identity{ID: "id-dave", Email: "dave@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens["onetimetoken"] = mockToken{id: "tok-ot", identityID: "id-dave", expiresAt: time.Now().Add(15 * time.Minute), createdAt: time.Now()}

	// First use.
	resp1 := doRequest(t, app, "GET", "/auth/signup/verify?token=onetimetoken", "")
	if resp1.StatusCode != 200 {
		t.Fatalf("first verify status = %d, want 200", resp1.StatusCode)
	}
	// Second use.
	resp2 := doRequest(t, app, "GET", "/auth/signup/verify?token=onetimetoken", "")
	if resp2.StatusCode != 410 {
		t.Fatalf("second verify status = %d, want 410", resp2.StatusCode)
	}
	b := bodyJSON(resp2)
	if b["error"] != "token already used" {
		t.Errorf("error = %v, want 'token already used'", b["error"])
	}
}

func TestMe_WithValidSession_Returns200(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	store.identities["eve@example.com"] = &signup.Identity{ID: "id-eve", Email: "eve@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}

	// Mint a valid session cookie manually.
	now := time.Now()
	payload := signup.SessionPayload{
		IdentityID: "id-eve",
		Email:      "eve@example.com",
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(24 * time.Hour).Unix(),
	}
	sessionVal, err := signup.SignSession(testCfg.SigningSecret, payload)
	if err != nil {
		t.Fatalf("SignSession error: %v", err)
	}

	cookie := &http.Cookie{Name: testCfg.SignupSessionCookieName, Value: sessionVal}
	resp := doRequest(t, app, "GET", "/auth/signup/me", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if b["email"] != "eve@example.com" {
		t.Errorf("email = %v, want eve@example.com", b["email"])
	}
}

func TestMe_NoCookie_Returns401(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	resp := doRequest(t, app, "GET", "/auth/signup/me", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMe_TamperedCookie_Returns401(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	cookie := &http.Cookie{Name: testCfg.SignupSessionCookieName, Value: "tampered.value.here"}
	resp := doRequest(t, app, "GET", "/auth/signup/me", "", cookie)
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMe_ExpiredSession_Returns401(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	past := time.Now().Add(-1 * time.Hour)
	payload := signup.SessionPayload{
		IdentityID: "id-ghost",
		Email:      "ghost@example.com",
		IssuedAt:   past.Add(-2 * time.Hour).Unix(),
		ExpiresAt:  past.Unix(), // already expired
	}
	sessionVal, _ := signup.SignSession(testCfg.SigningSecret, payload)
	cookie := &http.Cookie{Name: testCfg.SignupSessionCookieName, Value: sessionVal}
	resp := doRequest(t, app, "GET", "/auth/signup/me", "", cookie)
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
