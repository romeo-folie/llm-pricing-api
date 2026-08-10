//go:build integration

package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("pgxpool.New: %v — skipping", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v — skipping", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newTestRedis returns a Redis client for integration tests.
// When REDIS_URL is unset, defaults to localhost:6379.
// Skips the test if the resolved address is unreachable.
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
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 5 * time.Second
	opts.WriteTimeout = 5 * time.Second
	rc := redis.NewClient(opts)
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := rc.Ping(pingCtx).Err(); err != nil {
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

// mockMailerE2E captures sent emails and verify URLs (no-op delivery).
type mockMailerE2E struct {
	calls []string
	urls  []string // verify URLs passed to SendMagicLink
}

func (m *mockMailerE2E) SendMagicLink(_ context.Context, email, verifyURL string) error {
	m.calls = append(m.calls, email)
	m.urls = append(m.urls, verifyURL)
	return nil
}

// nopIssuer is a no-op KeyIssuer used in e2e tests that don't exercise key issuance.
type nopIssuer struct{}

func (nopIssuer) CreateKey(_ context.Context, _, _ string) (string, string, error) {
	return "test-key-id", "llmr_test_key", nil
}

func (nopIssuer) RevokeKey(_ context.Context, _ string) error { return nil }

// newE2EApp builds a Fiber app with real DB store, mock mailer, and optional
// IP rate limiting via real Redis.
func newE2EApp(t *testing.T, store auth.Store, mailer auth.Mailer, cfg auth.Config, rc *redis.Client) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	log := zerolog.Nop()
	h := auth.New(store, mailer, nopIssuer{}, nil, cfg, log)
	authGroup := app.Group("/auth")
	if rc != nil {
		authGroup.Use(middleware.IPRateLimit(rc, log))
	}
	auth.Register(authGroup, h)
	return app
}

// newE2EAppWithRLConfig is like newE2EApp but accepts an IPRateLimitConfig
// so tests can inject a fixed clock to avoid window-boundary flakiness.
func newE2EAppWithRLConfig(t *testing.T, store auth.Store, mailer auth.Mailer, cfg auth.Config, rc *redis.Client, rlCfg middleware.IPRateLimitConfig) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	log := zerolog.Nop()
	h := auth.New(store, mailer, nopIssuer{}, nil, cfg, log)
	authGroup := app.Group("/auth")
	if rc != nil {
		authGroup.Use(middleware.IPRateLimitWithConfig(rc, log, rlCfg))
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

func e2eBodyJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("e2eBodyJSON: failed to decode response body: %v", err)
	}
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
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email); err != nil {
			t.Logf("cleanup identity failed: %v", err)
		}
	})

	// Step 1: POST /auth/signup/request-link → 200.
	resp := e2eRequest(t, app, "POST", "/auth/signup/request-link",
		fmt.Sprintf(`{"email":"%s"}`, email))
	if resp.StatusCode != 200 {
		t.Fatalf("request-link: status = %d, want 200", resp.StatusCode)
	}

	// Step 2: Extract the raw token from the URL captured by the mock mailer.
	// This exercises the full RequestLink flow (identity upsert, token generation,
	// URL construction) instead of inserting a known token directly.
	if len(mailer.urls) == 0 {
		t.Fatal("request-link did not trigger SendMagicLink")
	}
	capturedURL := mailer.urls[len(mailer.urls)-1]
	parsedURL, err := url.Parse(capturedURL)
	if err != nil {
		t.Fatalf("parse captured verify URL: %v", err)
	}
	rawToken := parsedURL.Query().Get("token")
	if rawToken == "" {
		t.Fatalf("captured URL has no token query param: %s", capturedURL)
	}

	ident, err := store.GetIdentityByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetIdentityByEmail: %v", err)
	}

	// Step 3: GET /auth/signup/verify?token=... → 200 + session cookie.
	verifyResp := e2eRequest(t, app, "GET",
		"/auth/signup/verify?token="+rawToken, "")
	if verifyResp.StatusCode != 200 {
		b := e2eBodyJSON(t, verifyResp)
		t.Fatalf("verify: status = %d, want 200; body = %v", verifyResp.StatusCode, b)
	}

	verifyBody := e2eBodyJSON(t, verifyResp)
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
	meBody := e2eBodyJSON(t, meResp)
	if meBody["email"] != email {
		t.Errorf("me email = %v, want %s", meBody["email"], email)
	}
	if meBody["id"] != ident.ID {
		t.Errorf("me id = %v, want %s", meBody["id"], ident.ID)
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
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email); err != nil {
			t.Logf("cleanup identity failed: %v", err)
		}
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
		b := e2eBodyJSON(t, resp)
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
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email); err != nil {
			t.Logf("cleanup identity failed: %v", err)
		}
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
	b := e2eBodyJSON(t, resp2)
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

	// Use a fixed clock pinned to the middle of a 15-minute window so the
	// test never straddles a window boundary (eliminates wall-clock flakiness).
	windowSec := int64((15 * time.Minute).Seconds())
	midWindow := time.Unix((time.Now().Unix()/windowSec)*windowSec+windowSec/2, 0)
	fixedClock := func() time.Time { return midWindow }
	app := newE2EAppWithRLConfig(t, store, mailer, e2eCfg, rc, middleware.IPRateLimitConfig{TimeNow: fixedClock})

	// Flush rate limit keys scoped to the test IP.
	// Fiber test mode reports c.IP() as "0.0.0.0", which is what the
	// rate-limit middleware hashes. Use the same IP here so cleanup matches.
	// Use SCAN instead of KEYS — KEYS can be disabled or slow on managed Redis.
	ctx := context.Background()
	const fiberTestIP = "0.0.0.0"
	testIPHash := fmt.Sprintf("%x", sha256.Sum256([]byte(fiberTestIP)))[:16]
	rlPattern := fmt.Sprintf("iprl:%s:*", testIPHash)
	if keys, err := scanRedisKeys(ctx, rc, rlPattern); err != nil {
		t.Fatalf("pre-test Redis SCAN failed: %v", err)
	} else if len(keys) > 0 {
		if err := rc.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("pre-test Redis DEL failed: %v", err)
		}
	}
	t.Cleanup(func() {
		if keys, err := scanRedisKeys(ctx, rc, rlPattern); err != nil {
			t.Errorf("cleanup Redis SCAN failed: %v", err)
		} else if len(keys) > 0 {
			if err := rc.Del(ctx, keys...).Err(); err != nil {
				t.Errorf("cleanup Redis DEL failed: %v", err)
			}
		}
	})

	email := randEmail(t)
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE email = $1`, email); err != nil {
			t.Logf("cleanup identity failed: %v", err)
		}
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
	b := e2eBodyJSON(t, resp)
	if b["detail"] != "signup is currently disabled" {
		t.Errorf("detail = %v, want 'signup is currently disabled'", b["detail"])
	}
	if status, _ := b["status"].(float64); status != 503 {
		t.Errorf("status field = %v, want 503", status)
	}

	// Verify and Me endpoints should still work (they don't check the flag).
	// This ensures the flag only gates new signup requests.
	verifyResp := e2eRequest(t, app, "GET", "/auth/signup/verify?token=dummy", "")
	// Verify returns 401 (invalid token), NOT 503 — proving the flag doesn't block it.
	if verifyResp.StatusCode == 503 {
		t.Error("verify returned 503; the signup-disabled flag should not gate verify")
	}

	meResp := e2eRequest(t, app, "GET", "/auth/signup/me", "")
	// Me returns 401 (no session cookie), NOT 503.
	if meResp.StatusCode == 503 {
		t.Error("me returned 503; the signup-disabled flag should not gate me")
	}
}

// ── Test 6: Key regeneration ────────────────────────────────────────────────
// NOTE: POST /auth/signup/regenerate-key is now wired via auth.Register() in
// cmd/api/main.go (fixed in PR #137). Unit-level coverage (including RevokeKey
// call assertion) lives in handlers_test.go: TestRegenerateKey_WithExistingKey_RevokesOldAndIssuesNew.
// For RevokeAndInsertKey store behaviour, see internal/signup/store_integration_test.go.
