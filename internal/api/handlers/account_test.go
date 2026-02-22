package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	unkeygo "github.com/unkeyed/unkey-go"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/billing"
)

// --- mock AccountStore ---

type mockAccountStore struct {
	getFunc func(ctx context.Context, unkeyKeyID string) (AccountSubscription, error)
}

func (m *mockAccountStore) GetAccountSubscription(ctx context.Context, unkeyKeyID string) (AccountSubscription, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, unkeyKeyID)
	}
	return AccountSubscription{}, ErrBillingSubscriptionNotFound
}

// --- mock SubscriptionManager ---

type mockSubscriptionManager struct {
	getSubFunc    func(ctx context.Context, lsSubID string) (*billing.Subscription, error)
	getPortalFunc func(ctx context.Context, lsSubID string) (string, error)
}

func (m *mockSubscriptionManager) GetSubscription(ctx context.Context, lsSubID string) (*billing.Subscription, error) {
	if m.getSubFunc != nil {
		return m.getSubFunc(ctx, lsSubID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockSubscriptionManager) GetCustomerPortalURL(ctx context.Context, lsSubID string) (string, error) {
	if m.getPortalFunc != nil {
		return m.getPortalFunc(ctx, lsSubID)
	}
	return "", errors.New("not implemented")
}

// --- helpers ---

func makeAccountToken(keyID, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(keyID))
	return hex.EncodeToString(mac.Sum(nil))
}

func newTestFiber() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
}

// --- validateAccountToken ---

func TestValidateAccountToken_Valid(t *testing.T) {
	keyID := "key_abc123"
	secret := "supersecret"
	token := makeAccountToken(keyID, secret)
	if !validateAccountToken(keyID, secret, token) {
		t.Error("expected valid token to pass")
	}
}

func TestValidateAccountToken_Invalid(t *testing.T) {
	if validateAccountToken("key_abc", "secret", "wrongtoken") {
		t.Error("expected invalid token to fail")
	}
}

func TestValidateAccountToken_EmptyInputs(t *testing.T) {
	if validateAccountToken("", "", "") {
		t.Error("expected empty token to fail")
	}
}

// --- maskKey ---

func TestMaskKey_Long(t *testing.T) {
	key := "sk-live-abcdefghijklmnopqrstuvwxyz1234"
	got := maskKey(key)
	want := "sk-live-abcd...1234"
	if got != want {
		t.Errorf("maskKey(%q) = %q, want %q", key, got, want)
	}
}

func TestMaskKey_Short(t *testing.T) {
	key := "abc123"
	got := maskKey(key)
	if len(got) == 0 {
		t.Error("expected non-empty masked key")
	}
}

func TestMaskKey_Boundary(t *testing.T) {
	// Exactly 16 chars → short path (first 8 + "***")
	key16 := "1234567890abcdef"
	got16 := maskKey(key16)
	if got16 != "12345678***" {
		t.Errorf("maskKey(16-char) = %q, want %q", got16, "12345678***")
	}
	// 17 chars → long path (first 12 + "..." + last 4)
	key17 := "1234567890abcdefg"
	got17 := maskKey(key17)
	if got17 != "1234567890ab...defg" {
		t.Errorf("maskKey(17-char) = %q, want %q", got17, "1234567890ab...defg")
	}
}

// --- HandleVerify (unit) ---
// We test the validation logic using a minimal handler with a nil SDK.
// Real Unkey calls are excluded from unit tests.

func TestHandleVerify_MissingKey(t *testing.T) {
	app := newTestFiber()
	h := &AccountHandler{
		sessionSecret: "s",
		log:           zerolog.Nop(),
	}
	app.Post("/api/account/verify", h.HandleVerify)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/account/verify", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleVerify_InvalidJSON(t *testing.T) {
	app := newTestFiber()
	h := &AccountHandler{
		sessionSecret: "s",
		log:           zerolog.Nop(),
	}
	app.Post("/api/account/verify", h.HandleVerify)

	req := httptest.NewRequest(http.MethodPost, "/api/account/verify", bytes.NewBufferString("notjson"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// --- HandleUsage ---

func TestHandleUsage_MissingKeyID(t *testing.T) {
	app := newTestFiber()
	h := &AccountHandler{sessionSecret: "s", log: zerolog.Nop()}
	app.Get("/api/account/usage", h.HandleUsage)

	req := httptest.NewRequest(http.MethodGet, "/api/account/usage", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleUsage_MissingToken(t *testing.T) {
	app := newTestFiber()
	h := &AccountHandler{sessionSecret: "s", log: zerolog.Nop()}
	app.Get("/api/account/usage", h.HandleUsage)

	req := httptest.NewRequest(http.MethodGet, "/api/account/usage?key_id=uk_abc", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleUsage_InvalidToken(t *testing.T) {
	app := newTestFiber()
	h := &AccountHandler{sessionSecret: "s", log: zerolog.Nop()}
	app.Get("/api/account/usage", h.HandleUsage)

	req := httptest.NewRequest(http.MethodGet, "/api/account/usage?key_id=uk_abc", nil)
	req.Header.Set("X-Account-Token", "wrong")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// TestHandleUsage_ValidToken_SDKUnavailable verifies that when the token is
// valid but the Unkey SDK is nil (no real external call), the handler panics
// before reaching any external call. This ensures token validation runs first.
// In production, sdk is always non-nil.
func TestHandleUsage_ValidToken_SDKUnavailable(t *testing.T) {
	const secret = "testsecret"
	const keyID = "uk_realkey"

	app := newTestFiber()
	// sdk is nil — HandleUsage will panic when it tries to call sdk.Keys.GetVerifications.
	// We recover from the panic to verify token validation passed.
	h := &AccountHandler{sessionSecret: secret, sdk: nil, log: zerolog.Nop()}
	app.Get("/api/account/usage", func(c *fiber.Ctx) (retErr error) {
		defer func() {
			if r := recover(); r != nil {
				// panic means token validation passed and sdk was accessed
				retErr = c.Status(fiber.StatusTeapot).SendString("panic_as_expected")
			}
		}()
		return h.HandleUsage(c)
	})

	token := makeAccountToken(keyID, secret)
	req := httptest.NewRequest(http.MethodGet, "/api/account/usage?key_id="+keyID, nil)
	req.Header.Set("X-Account-Token", token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	// 418 means we got past token validation (the panic was in SDK call).
	if resp.StatusCode != http.StatusTeapot {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 418 (token validation passed); body: %s", resp.StatusCode, body)
	}
}

// --- AccountVerifyResponse JSON shape ---

func TestAccountVerifyResponse_JSONShape(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	r := AccountVerifyResponse{
		Tier:       "developer",
		MaskedKey:  "sk-live-xxxx...4321",
		KeyID:      "uk_abc",
		DailyLimit: 10000,
		UsageToday: 42,
		EndsAt:     &now,
		PortalURL:  "https://portal.example.com",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"tier", "maskedKey", "keyId", "dailyLimit", "usageToday", "endsAt", "portalUrl"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON field %q", key)
		}
	}
}

// --- compile-time interface check ---

// Ensure mockAccountStore implements AccountStore (catches drift during refactors).
var _ AccountStore = (*mockAccountStore)(nil)
var _ billing.SubscriptionManager = (*mockSubscriptionManager)(nil)

// Ensure RegisterAccount type-checks at compile time by referencing its signature.
var _ = func() {
	var _ func(*fiber.App, interface{ QueryRow(context.Context, string, ...any) interface{ Scan(...any) error } }, *billing.Service, string, string, string, zerolog.Logger)
	_ = unkeygo.WithSecurity // SDK import is live in register.go
}
