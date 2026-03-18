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
	keys       []*signup.KeyRecord         // append-only; active = last with status "active"
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

func (m *mockStore) GetActiveKey(_ context.Context, identityID string) (*signup.KeyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.keys {
		if k.IdentityID == identityID && k.Status == "active" {
			return k, nil
		}
	}
	return nil, signup.ErrNotFound
}

func (m *mockStore) InsertKey(_ context.Context, identityID, providerKeyID string) (*signup.KeyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := &signup.KeyRecord{
		ID:            "key-" + providerKeyID,
		IdentityID:    identityID,
		ProviderKeyID: providerKeyID,
		Status:        "active",
		CreatedAt:     time.Now(),
	}
	m.keys = append(m.keys, k)
	return k, nil
}

func (m *mockStore) RevokeAndInsertKey(_ context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*signup.KeyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mark old key revoked if present.
	for i, k := range m.keys {
		if k.IdentityID == identityID && k.ProviderKeyID == oldProviderKeyID {
			now := time.Now()
			m.keys[i].Status = "revoked"
			m.keys[i].RevokedAt = &now
		}
	}
	// Insert new key.
	k := &signup.KeyRecord{
		ID:            "key-" + newProviderKeyID,
		IdentityID:    identityID,
		ProviderKeyID: newProviderKeyID,
		Status:        "active",
		CreatedAt:     time.Now(),
	}
	m.keys = append(m.keys, k)
	return k, nil
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

// mockIssuer is a no-op KeyIssuer for tests that don't exercise key issuance.
type mockIssuer struct {
	createKeyID  string
	createKey    string
	createErr    error
	revokeErr    error
}

func (m *mockIssuer) CreateKey(_ context.Context, _, _ string) (string, string, error) {
	if m.createErr != nil {
		return "", "", m.createErr
	}
	id := m.createKeyID
	if id == "" {
		id = "test-provider-key-id"
	}
	k := m.createKey
	if k == "" {
		k = "llmr_test_plaintext_key"
	}
	return id, k, nil
}

func (m *mockIssuer) RevokeKey(_ context.Context, _ string) error {
	return m.revokeErr
}

func newTestApp(store auth.Store, mailer auth.Mailer) *fiber.App {
	return newTestAppWithIssuer(store, mailer, &mockIssuer{})
}

func newTestAppWithIssuer(store auth.Store, mailer auth.Mailer, issuer auth.KeyIssuer) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	log := zerolog.Nop()
	h := auth.New(store, mailer, issuer, testCfg, log)
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

func bodyJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("bodyJSON: failed to decode response body: %v", err)
	}
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
	b := bodyJSON(t, resp)
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
	b := bodyJSON(t, resp)
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

func TestVerify_ValidToken_Redirects302AndSetsCookie(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)

	// Seed identity + token directly. Store keyed by hash (handler hashes before lookup).
	store.identities["bob@example.com"] = &signup.Identity{ID: "id-bob", Email: "bob@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens[signup.HashToken("validtoken123")] = mockToken{id: "tok-1", identityID: "id-bob", expiresAt: time.Now().Add(15 * time.Minute), createdAt: time.Now()}

	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=validtoken123", "")
	if resp.StatusCode != 302 {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != testCfg.MagicLinkBaseURL+"/signup/verified" {
		t.Errorf("Location = %q, want /signup/verified redirect", loc)
	}
	// Check session cookie is set on the redirect response.
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
		t.Error("session cookie not set on verify redirect response")
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

func TestVerify_InvalidToken_RedirectsWithError(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=doesnotexist", "")
	if resp.StatusCode != 302 {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != testCfg.MagicLinkBaseURL+"/signup/free?error=invalid-link" {
		t.Errorf("Location = %q, want invalid-link redirect", loc)
	}
}

func TestVerify_ExpiredToken_RedirectsWithError(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	store.identities["carol@example.com"] = &signup.Identity{ID: "id-carol", Email: "carol@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens[signup.HashToken("expiredtok")] = mockToken{id: "tok-exp", identityID: "id-carol", expiresAt: time.Now().Add(-1 * time.Minute), createdAt: time.Now()}
	resp := doRequest(t, app, "GET", "/auth/signup/verify?token=expiredtok", "")
	if resp.StatusCode != 302 {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != testCfg.MagicLinkBaseURL+"/signup/free?error=link-expired" {
		t.Errorf("Location = %q, want link-expired redirect", loc)
	}
}

func TestVerify_ReusedToken_RedirectsWithError(t *testing.T) {
	store := newMockStore()
	mailer := &mockMailer{}
	app := newTestApp(store, mailer)
	store.identities["dave@example.com"] = &signup.Identity{ID: "id-dave", Email: "dave@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.tokens[signup.HashToken("onetimetoken")] = mockToken{id: "tok-ot", identityID: "id-dave", expiresAt: time.Now().Add(15 * time.Minute), createdAt: time.Now()}

	// First use — expect redirect to /signup/verified.
	resp1 := doRequest(t, app, "GET", "/auth/signup/verify?token=onetimetoken", "")
	if resp1.StatusCode != 302 {
		t.Fatalf("first verify status = %d, want 302", resp1.StatusCode)
	}
	if loc := resp1.Header.Get("Location"); loc != testCfg.MagicLinkBaseURL+"/signup/verified" {
		t.Errorf("first verify Location = %q, want /signup/verified redirect", loc)
	}
	// Second use — expect redirect to /signup/free?error=link-used.
	resp2 := doRequest(t, app, "GET", "/auth/signup/verify?token=onetimetoken", "")
	if resp2.StatusCode != 302 {
		t.Fatalf("second verify status = %d, want 302", resp2.StatusCode)
	}
	if loc := resp2.Header.Get("Location"); loc != testCfg.MagicLinkBaseURL+"/signup/free?error=link-used" {
		t.Errorf("second verify Location = %q, want link-used redirect", loc)
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
	b := bodyJSON(t, resp)
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
	h := auth.New(store, mailer, &mockIssuer{}, disabledCfg, log)
	authGroup := app.Group("/auth")
	auth.Register(authGroup, h)

	resp := doRequest(t, app, "POST", "/auth/signup/request-link", `{"email":"alice@example.com"}`)
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	b := bodyJSON(t, resp)
	// Response is now RFC7807 ProblemDetail — check "detail" field.
	if b["detail"] != "signup is currently disabled" {
		t.Errorf("detail = %v, want 'signup is currently disabled'", b["detail"])
	}
	if b["status"] != float64(503) {
		t.Errorf("status = %v, want 503", b["status"])
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

// ── IssueKey ──────────────────────────────────────────────────────────────────

func TestIssueKey_NoSession_Returns401(t *testing.T) {
	store := newMockStore()
	app := newTestApp(store, &mockMailer{})
	resp := doRequest(t, app, "POST", "/auth/signup/issue-key", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestIssueKey_NewIdentity_IssuesKey(t *testing.T) {
	store := newMockStore()
	issuer := &mockIssuer{createKeyID: "prov-abc", createKey: "llmr_plaintext"}
	app := newTestAppWithIssuer(store, &mockMailer{}, issuer)

	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	// Seed identity so GetIdentityByID succeeds.
	store.identities["alice@example.com"] = &signup.Identity{
		ID:    "id-alice",
		Email: "alice@example.com",
	}

	resp := doRequest(t, app, "POST", "/auth/signup/issue-key", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var b map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b["plaintext"] != "llmr_plaintext" {
		t.Errorf("plaintext = %v, want llmr_plaintext", b["plaintext"])
	}
	if b["provider_key_id"] != "prov-abc" {
		t.Errorf("provider_key_id = %v, want prov-abc", b["provider_key_id"])
	}
}

func TestIssueKey_ExistingKey_ReturnsMetadata(t *testing.T) {
	store := newMockStore()
	// Pre-populate a key in the store.
	now := time.Now()
	store.keys = append(store.keys, &signup.KeyRecord{
		ID:            "k1",
		IdentityID:    "id-alice",
		ProviderKeyID: "existing-prov",
		Status:        "active",
		CreatedAt:     now,
	})
	// Override GetActiveKey to return the seeded key.
	store.identities["alice@example.com"] = &signup.Identity{
		ID:    "id-alice",
		Email: "alice@example.com",
	}

	// mockStore.GetActiveKey now scans m.keys, so no wrapper needed.
	issuer := &mockIssuer{}
	app := newTestAppWithIssuer(store, &mockMailer{}, issuer)

	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	resp := doRequest(t, app, "POST", "/auth/signup/issue-key", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var b map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b["status"] != "existing" {
		t.Errorf("status = %v, want existing", b["status"])
	}
	if b["plaintext"] != nil {
		t.Errorf("plaintext should not be present for existing key response")
	}
}

// ── RegenerateKey ─────────────────────────────────────────────────────────────

func TestRegenerateKey_NoSession_Returns401(t *testing.T) {
	store := newMockStore()
	app := newTestApp(store, &mockMailer{})
	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", "")
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRegenerateKey_IssuesNewKey(t *testing.T) {
	store := newMockStore()
	store.identities["alice@example.com"] = &signup.Identity{
		ID:    "id-alice",
		Email: "alice@example.com",
	}
	issuer := &mockIssuer{createKeyID: "new-prov", createKey: "llmr_new_key"}
	app := newTestAppWithIssuer(store, &mockMailer{}, issuer)

	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 (got %d)", resp.StatusCode, resp.StatusCode)
	}
	var b map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b["plaintext"] != "llmr_new_key" {
		t.Errorf("plaintext = %v, want llmr_new_key", b["plaintext"])
	}
}

// ── Test Me includes has_active_key ──────────────────────────────────────────

func TestMe_IncludesHasActiveKey(t *testing.T) {
	store := newMockStore()
	store.identities["alice@example.com"] = &signup.Identity{
		ID:    "id-alice",
		Email: "alice@example.com",
	}
	app := newTestApp(store, &mockMailer{})

	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	resp := doRequest(t, app, "GET", "/auth/signup/me", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var b map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// mockStore.GetActiveKey always returns ErrNotFound → has_active_key = false
	if b["has_active_key"] != false {
		t.Errorf("has_active_key = %v, want false", b["has_active_key"])
	}
	if b["email_verified"] == nil {
		t.Error("email_verified field missing from /me response")
	}
	if _, ok := b["id"]; !ok {
		t.Error("id field missing from /me response")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// makeSessionCookie builds a signed session cookie using testCfg.
func makeSessionCookie(t *testing.T, identityID, email string) *http.Cookie {
	t.Helper()
	now := time.Now()
	payload := signup.SessionPayload{
		IdentityID: identityID,
		Email:      email,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(24 * time.Hour).Unix(),
	}
	val, err := signup.SignSession(testCfg.SigningSecret, payload)
	if err != nil {
		t.Fatalf("makeSessionCookie: %v", err)
	}
	return &http.Cookie{Name: testCfg.SignupSessionCookieName, Value: val}
}

// TestRegenerateKey_WithExistingKey_RevokesOldAndIssuesNew tests the full regenerate path
// when the identity already has an active key.
func TestRegenerateKey_WithExistingKey_RevokesOldAndIssuesNew(t *testing.T) {
	store := newMockStore()
	store.identities["alice@example.com"] = &signup.Identity{
		ID:    "id-alice",
		Email: "alice@example.com",
	}
	// Seed an existing active key.
	store.keys = append(store.keys, &signup.KeyRecord{
		ID:            "k-old",
		IdentityID:    "id-alice",
		ProviderKeyID: "old-prov-id",
		Status:        "active",
		CreatedAt:     time.Now().Add(-1 * time.Hour),
	})

	var revokedKeys []string
	issuer := &mockIssuer{createKeyID: "new-prov-id", createKey: "llmr_new_key"}
	issuer.revokeErr = nil
	// Track revoke calls via a custom issuer
	type trackingIssuer struct {
		mockIssuer
		revoked []string
	}
	ti := &trackingIssuer{mockIssuer: *issuer}
	// Use a closure-based approach via the real mockIssuer — enough to verify the flow.
	_ = revokedKeys // just verify new key is in response

	app := newTestAppWithIssuer(store, &mockMailer{}, &mockIssuer{createKeyID: "new-prov-id", createKey: "llmr_new_key"})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", "", cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var b map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b["plaintext"] != "llmr_new_key" {
		t.Errorf("plaintext = %v, want llmr_new_key", b["plaintext"])
	}
	if b["provider_key_id"] != "new-prov-id" {
		t.Errorf("provider_key_id = %v, want new-prov-id", b["provider_key_id"])
	}
	// Old key should be marked revoked in the store.
	found := false
	for _, k := range store.keys {
		if k.ProviderKeyID == "old-prov-id" && k.Status == "revoked" {
			found = true
		}
	}
	if !found {
		t.Error("old key should be revoked in store after regenerate")
	}
	_ = ti
}
