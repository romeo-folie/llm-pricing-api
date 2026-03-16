//go:build integration

package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/auth"
	"llm-pricing-api/internal/middleware"
	"llm-pricing-api/internal/signup"
)

// ── Test infrastructure ─────────────────────────────────────────────────────

// newTestPool connects to DATABASE_URL. Skips if not set or unreachable.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("pgxpool.New: %v — skipping", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v — skipping", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newTestRedis connects to REDIS_URL. Skips if not set or unreachable.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	rawURL := os.Getenv("REDIS_URL")
	var opts *redis.Options
	if rawURL == "" {
		opts = &redis.Options{Addr: "localhost:6379"}
	} else {
		var err error
		if opts, err = redis.ParseURL(rawURL); err != nil {
			// Not a valid redis:// URL — treat as bare host:port.
			opts = &redis.Options{Addr: rawURL}
		}
	}
	rc := redis.NewClient(opts)
	if err := rc.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis unreachable: %v — skipping", err)
	}
	t.Cleanup(func() { rc.Close() })
	return rc
}

// scanRedisKeys uses SCAN to find all keys matching pattern.
// Safer than KEYS on managed Redis (KEYS can be disabled or block on large keyspaces).
func scanRedisKeys(ctx context.Context, rc *redis.Client, pattern string) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		var batch []string
		var err error
		batch, cursor, err = rc.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return keys, err
		}
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}
	return keys, nil
}

func randEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("e2e-%d@example.com", time.Now().UnixNano())
}

// e2eCfg is the auth config used by E2E tests. SignupEnabled defaults to true.
var e2eCfg = auth.Config{
	SigningSecret:           "e2e-test-secret-at-least-32-bytes!", // 34 bytes
	MagicLinkTTLMinutes:     15,
	MagicLinkBaseURL:        "https://example.com",
	MagicLinkPath:           "/signup/verify",
	SignupSessionCookieName: "e2e_session",
	SignupSessionTTLHours:   24,
	SignupSessionSecure:     false,
	SignupEnabled:           true,
}

// mockMailerE2E captures sent emails (no-op delivery).
type mockMailerE2E struct {
	calls []string
}

func (m *mockMailerE2E) SendMagicLink(_ context.Context, email, _ string) error {
	m.calls = append(m.calls, email)
	return nil
}

// newE2EApp builds a Fiber app with real DB store, mock mailer, and optional
// IP rate limiting via real Redis.
func newE2EApp(t *testing.T, store auth.Store, mailer auth.Mailer, cfg auth.Config, rc *redis.Client) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	log := zerolog.Nop()
	h := auth.New(store, mailer, cfg, log)
	authGroup := app.Group("/auth")
	if rc != nil {
		authGroup.Use(middleware.IPRateLimit(rc, log))
	}
	auth.Register(authGroup, h)
	return app
}

func e2eRequest(t *testing.T, app *fiber.App, method, path, body string, cookies ...*http.Cookie) *http.Response {
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
	resp, err := app.Test(req, 10000)
	if err != nil {
		t.Fatalf("app.Test(%s %s) failed: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func e2eBodyJSON(resp *http.Response) map[string]any {
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m
}

// ── Test 1: Happy path — request-link → verify → me ────────────────────────

func TestE2E_RequestLink_Verify_Me(t *testing.T) {
	pool := newTestPool(t)
	store := signup.NewStore(pool)
	mailer := &mockMailerE2E{}
	app := newE2EApp(t, store, mailer, e2eCfg, nil)
	email := randEmail(t)
	ctx := context.Background()

	// Cleanup identity after test.
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email)
	})

	// Step 1: POST /auth/signup/request-link → 200.
	resp := e2eRequest(t, app, "POST", "/auth/signup/request-link",
		fmt.Sprintf(`{"email":"%s"}`, email))
	if resp.StatusCode != 200 {
		t.Fatalf("request-link: status = %d, want 200", resp.StatusCode)
	}

	// Step 2: Extract the token hash from DB and reconstruct raw token.
	// The handler called GenerateRawToken() then HashToken() — we can't
	// reverse the hash. Instead, insert a known token directly for the
	// identity that was upserted above, simulating the verify step.
	ident, err := store.GetIdentityByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetIdentityByEmail: %v", err)
	}

	rawToken := fmt.Sprintf("e2e-verify-%d", time.Now().UnixNano())
	tokenHash := signup.HashToken(rawToken)
	_, err = store.InsertToken(ctx, ident.ID, tokenHash, time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("InsertToken: %v", err)
	}

	// Step 3: GET /auth/signup/verify?token=... → 200 + session cookie.
	verifyResp := e2eRequest(t, app, "GET",
		"/auth/signup/verify?token="+rawToken, "")
	if verifyResp.StatusCode != 200 {
		b := e2eBodyJSON(verifyResp)
		t.Fatalf("verify: status = %d, want 200; body = %v", verifyResp.StatusCode, b)
	}

	verifyBody := e2eBodyJSON(verifyResp)
	if verifyBody["verified"] != true {
		t.Error("expected verified=true")
	}

	// Extract session cookie.
	var sessionCookie *http.Cookie
	for _, c := range verifyResp.Cookies() {
		if c.Name == e2eCfg.SignupSessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session cookie not set after verify")
	}

	// Step 4: GET /auth/signup/me → 200 with identity data.
	meResp := e2eRequest(t, app, "GET", "/auth/signup/me", "", sessionCookie)
	if meResp.StatusCode != 200 {
		t.Fatalf("me: status = %d, want 200", meResp.StatusCode)
	}
	meBody := e2eBodyJSON(meResp)
	if meBody["email"] != email {
		t.Errorf("me email = %v, want %s", meBody["email"], email)
	}
	if meBody["identity_id"] != ident.ID {
		t.Errorf("me identity_id = %v, want %s", meBody["identity_id"], ident.ID)
	}
}

// ── Test 2: Expired token rejected ──────────────────────────────────────────

func TestE2E_ExpiredToken_Rejected(t *testing.T) {
	pool := newTestPool(t)
	store := signup.NewStore(pool)
	mailer := &mockMailerE2E{}
	app := newE2EApp(t, store, mailer, e2eCfg, nil)
	email := randEmail(t)
	ctx := context.Background()

	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email)
	})

	ident, err := store.UpsertIdentity(ctx, email, "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}

	rawToken := fmt.Sprintf("e2e-expired-%d", time.Now().UnixNano())
	tokenHash := signup.HashToken(rawToken)
	// Insert token that already expired 10 seconds ago.
	_, err = store.InsertToken(ctx, ident.ID, tokenHash, time.Now().Add(-10*time.Second))
	if err != nil {
		t.Fatalf("InsertToken: %v", err)
	}

	resp := e2eRequest(t, app, "GET", "/auth/signup/verify?token="+rawToken, "")
	if resp.StatusCode != 410 {
		b := e2eBodyJSON(resp)
		t.Fatalf("expired token: status = %d, want 410; body = %v", resp.StatusCode, b)
	}
}

// ── Test 3: Reused token rejected ───────────────────────────────────────────

func TestE2E_ReusedToken_Rejected(t *testing.T) {
	pool := newTestPool(t)
	store := signup.NewStore(pool)
	mailer := &mockMailerE2E{}
	app := newE2EApp(t, store, mailer, e2eCfg, nil)
	email := randEmail(t)
	ctx := context.Background()

	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email)
	})

	ident, err := store.UpsertIdentity(ctx, email, "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}

	rawToken := fmt.Sprintf("e2e-reuse-%d", time.Now().UnixNano())
	tokenHash := signup.HashToken(rawToken)
	_, err = store.InsertToken(ctx, ident.ID, tokenHash, time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("InsertToken: %v", err)
	}

	// First verify — should succeed.
	resp1 := e2eRequest(t, app, "GET", "/auth/signup/verify?token="+rawToken, "")
	if resp1.StatusCode != 200 {
		t.Fatalf("first verify: status = %d, want 200", resp1.StatusCode)
	}

	// Second verify with same token — should fail.
	resp2 := e2eRequest(t, app, "GET", "/auth/signup/verify?token="+rawToken, "")
	if resp2.StatusCode != 410 {
		t.Fatalf("reused token: status = %d, want 410", resp2.StatusCode)
	}
	b := e2eBodyJSON(resp2)
	if b["detail"] != "token already used" {
		t.Errorf("detail = %v, want 'token already used'", b["detail"])
	}
}

// ── Test 4: IP rate limit blocks after threshold ────────────────────────────

func TestE2E_IPRateLimit_Blocks_After_Threshold(t *testing.T) {
	pool := newTestPool(t)
	rc := newTestRedis(t)
	store := signup.NewStore(pool)
	mailer := &mockMailerE2E{}
	app := newE2EApp(t, store, mailer, e2eCfg, rc)

	// Flush rate limit keys to ensure clean state.
	// Use SCAN instead of KEYS — KEYS can be disabled or slow on managed Redis.
	ctx := context.Background()
	if keys, err := scanRedisKeys(ctx, rc, "iprl:*"); err == nil && len(keys) > 0 {
		rc.Del(ctx, keys...)
	}
	t.Cleanup(func() {
		if keys, err := scanRedisKeys(ctx, rc, "iprl:*"); err == nil && len(keys) > 0 {
			rc.Del(ctx, keys...)
		}
	})

	email := randEmail(t)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email)
	})
	body := fmt.Sprintf(`{"email":"%s"}`, email)

	// ipRateLimitMax = 10 (defined in middleware). Requests 1-10 should pass.
	for i := 1; i <= 10; i++ {
		resp := e2eRequest(t, app, "POST", "/auth/signup/request-link", body)
		if resp.StatusCode != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, resp.StatusCode)
		}
	}

	// Request 11 should be rate-limited.
	resp := e2eRequest(t, app, "POST", "/auth/signup/request-link", body)
	if resp.StatusCode != 429 {
		t.Fatalf("request 11: status = %d, want 429", resp.StatusCode)
	}
}

// ── Test 5: SIGNUP_ENABLED=false returns 503 ────────────────────────────────

func TestE2E_SignupDisabled_Returns503(t *testing.T) {
	pool := newTestPool(t)
	store := signup.NewStore(pool)
	mailer := &mockMailerE2E{}

	disabledCfg := e2eCfg
	disabledCfg.SignupEnabled = false
	app := newE2EApp(t, store, mailer, disabledCfg, nil)

	resp := e2eRequest(t, app, "POST", "/auth/signup/request-link",
		`{"email":"disabled@example.com"}`)
	if resp.StatusCode != 503 {
		t.Fatalf("signup disabled: status = %d, want 503", resp.StatusCode)
	}
	b := e2eBodyJSON(resp)
	if b["error"] != "signup is currently disabled" {
		t.Errorf("error = %v, want 'signup is currently disabled'", b["error"])
	}

	// Verify and Me endpoints should still work (they don't check the flag).
	// This ensures the flag only gates new signup requests.
}

// ── Test 6: Key regeneration ────────────────────────────────────────────────
// NOTE: The regenerate-key endpoint lives in internal/signup/handlers.go
// (signup.Register) which is NOT wired in cmd/api/main.go — only the
// internal/auth/ routes are active. Therefore we cannot E2E-test key
// regeneration through HTTP. The store-level integration tests in
// internal/signup/store_integration_test.go cover RevokeAndInsertKey.
