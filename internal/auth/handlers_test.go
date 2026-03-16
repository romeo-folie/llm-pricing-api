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

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/auth"
	"llm-pricing-api/internal/signup"
)

// ── Mock store ──────────────────────────────────────────────────────────────────

// mockStore implements auth.Store with an in-memory backend.
type mockStore struct {
	mu         sync.Mutex
	identities map[string]*signup.Identity // keyed by email
	tokens     map[string]mockToken        // keyed by tokenHash (SHA-256 of rawToken)
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

func (m *mockStore) UpsertIdentity(_ context.Context, email, _, _ string) (*signup.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.identities[email]; ok {
		return existing, nil
	}
	now := time.Now()
	id := &signup.Identity{
		ID:        fmt.Sprintf("id-%s", email),
		Email:     email,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.identities[email] = id
	return id, nil
}

func (m *mockStore) GetIdentityByEmail(_ context.Context, email string) (*signup.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ident, ok := m.identities[email]; ok {
		return ident, nil
	}
	return nil, signup.ErrNotFound
}

func (m *mockStore) GetIdentityByID(_ context.Context, id string) (*signup.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ident := range m.identities {
		if ident.ID == id {
			return ident, nil
		}
	}
	return nil, signup.ErrNotFound
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

// InsertToken stores the token hash (not the raw token) keyed by tokenHash.
func (m *mockStore) InsertToken(_ context.Context, identityID, tokenHash string, expiresAt time.Time) (*signup.MagicLinkToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok := mockToken{
		id:         "tok-" + tokenHash,
		identityID: identityID,
		expiresAt:  expiresAt,
		createdAt:  time.Now(),
	}
	m.tokens[tokenHash] = tok
	return &signup.MagicLinkToken{
		ID:         tok.id,
		IdentityID: identityID,
		TokenHash:  tokenHash,
		ExpiresAt:  expiresAt,
		CreatedAt:  tok.createdAt,
	}, nil
}

// ConsumeToken looks up by tokenHash (the hashed form stored at InsertToken time).
func (m *mockStore) ConsumeToken(_ context.Context, tokenHash string) (*signup.MagicLinkToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, ok := m.tokens[tokenHash]
	if !ok {
		return nil, signup.ErrNotFound
	}
	if tok.usedAt != nil {
		return nil, signup.ErrTokenConsumed
	}
	if tok.expiresAt.Before(time.Now()) {
		return nil, signup.ErrTokenExpired
	}
	now := time.Now()
	tok.usedAt = &now
	m.tokens[tokenHash] = tok
	return &signup.MagicLinkToken{
		ID:         tok.id,
		IdentityID: tok.identityID,
		TokenHash:  tokenHash,
		ExpiresAt:  tok.expiresAt,
		UsedAt:     tok.usedAt,
		CreatedAt:  tok.createdAt,
	}, nil
}

func (m *mockStore) GetActiveKey(_ context.Context, _ string) (*signup.KeyRecord, error) {
	return nil, signup.ErrNotFound
}

func (m *mockStore) InsertKey(_ context.Context, identityID, providerKeyID string) (*signup.KeyRecord, error) {
	return &signup.KeyRecord{
		ID:            "key-" + providerKeyID,
		IdentityID:    identityID,
		ProviderKeyID: providerKeyID,
		Status:        "active",
		CreatedAt:     time.Now(),
	}, nil
}

func (m *mockStore) DeleteExpiredTokens(_ context.Context) (int64, error) {
	return 0, nil
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
	SignupEnabled:           true,
}

func newTestApp(store auth.Store, mailer auth.Mailer) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
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
	t.Cleanup(func() { resp.Body.Close() })
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
	if detail, ok := b["detail"]; !ok || detail == "" {
		t.Error("expected non-empty 'detail' field in response body")
	}
}

// TestRequestLink_MultipleRequests_AllReturn200 verifies that repeated
// request-link calls always return 200 (no account enumeration leak).
// Per-identity token rate-limiting was moved to the IP rate-limit middleware
// on the route group; the handler itself no longer caps token creation.
func TestRequestLink_MultipleRequests_AllReturn200(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)

	for i := 0; i < 5; i++ {
		resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"repeat@example.com"}`)
		if resp.StatusCode != 200 {
			t.Fatalf("request %d: status = %d, want 200", i+1, resp.StatusCode)
		}
	}
}

func TestVerify_ValidToken_Returns200AndSetsCookie(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)

	// Seed identity + token directly. Store keyed by hash (handler hashes before lookup).
	store.identities["bob@example.com"] = &signup.Identity{ID: "id-bob", Email: "bob@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens[signup.HashToken("validtoken123")] = mockToken{id: "tok-1", identityID: "id-bob", expiresAt: time.Now().Add(15 * time.Minute), createdAt: time.Now()}

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
	store.tokens[signup.HashToken("expiredtok")] = mockToken{id: "tok-exp", identityID: "id-carol", expiresAt: time.Now().Add(-1 * time.Minute), createdAt: time.Now()}
	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=expiredtok", "")
	if resp.StatusCode != 410 {
		t.Fatalf("status = %d, want 410", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if b["detail"] != "token expired" {
		t.Errorf("detail = %v, want 'token expired'", b["detail"])
	}
}

func TestVerify_ReusedToken_Returns410(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	store.identities["dave@example.com"] = &signup.Identity{ID: "id-dave", Email: "dave@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens[signup.HashToken("onetimetoken")] = mockToken{id: "tok-ot", identityID: "id-dave", expiresAt: time.Now().Add(15 * time.Minute), createdAt: time.Now()}

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
	if b["detail"] != "token already used" {
		t.Errorf("detail = %v, want 'token already used'", b["detail"])
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

func TestRequestLink_SignupDisabled_Returns503(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	disabledCfg := testCfg
	disabledCfg.SignupEnabled = false
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	log := zerolog.Nop()
	h := auth.New(store, mailer, disabledCfg, log)
	authGroup := app.Group("/auth")
	auth.Register(authGroup, h)

	resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"alice@example.com"}`)
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	b := bodyJSON(resp)
	if b["error"] != "signup is currently disabled" {
		t.Errorf("error = %v, want 'signup is currently disabled'", b["error"])
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
	sessionVal, err := signup.SignSession(testCfg.SigningSecret, payload)
	if err != nil {
		t.Fatalf("SignSession: %v", err)
	}
	cookie := &http.Cookie{Name: testCfg.SignupSessionCookieName, Value: sessionVal}
	resp := doRequest(t, app, "GET", "/auth/signup/me", "", cookie)
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
