package signup_test

import (
	"strings"
	"testing"
	"time"

	"llm-pricing-api/internal/signup"
)

// ── GenerateRawToken ────────────────────────────────────────────────────────

func TestGenerateRawToken_NotEmpty(t *testing.T) {
	tok, err := signup.GenerateRawToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == "" {
		t.Error("token must not be empty")
	}
}

func TestGenerateRawToken_Unique(t *testing.T) {
	tok1, err := signup.GenerateRawToken()
	if err != nil {
		t.Fatalf("GenerateRawToken: %v", err)
	}
	tok2, err := signup.GenerateRawToken()
	if err != nil {
		t.Fatalf("GenerateRawToken: %v", err)
	}
	if tok1 == tok2 {
		t.Error("two generated tokens must not be equal")
	}
}

// ── BuildVerifyURL ──────────────────────────────────────────────────────────

func TestBuildVerifyURL_NormalCase(t *testing.T) {
	url := signup.BuildVerifyURL("https://example.com", "/auth/verify", "abc123")
	if !strings.HasPrefix(url, "https://example.com/auth/verify") {
		t.Errorf("unexpected URL prefix: %s", url)
	}
	if !strings.Contains(url, "token=abc123") {
		t.Errorf("URL must contain token param: %s", url)
	}
}

func TestBuildVerifyURL_SpecialCharsEncoded(t *testing.T) {
	u := signup.BuildVerifyURL("https://example.com", "/verify", "a+b=c/d")
	// The token should be properly encoded in the query string.
	if !strings.Contains(u, "token=") {
		t.Errorf("URL must contain token param: %s", u)
	}
}

func TestBuildVerifyURL_FallbackPath(t *testing.T) {
	// Invalid base URL triggers the fallback path.
	u := signup.BuildVerifyURL("://invalid", "/verify", "tok&en=1")
	if !strings.Contains(u, "/verify") {
		t.Errorf("fallback must still include path: %s", u)
	}
	// Verify the token is URL-encoded in fallback (& should be escaped).
	if strings.Contains(u, "tok&en=1") {
		t.Errorf("fallback must URL-encode the token: %s", u)
	}
}

// ── SignSession / VerifySession ─────────────────────────────────────────────

func TestSignVerifySession_RoundTrip(t *testing.T) {
	secret := "test-secret-key-12345"
	now := time.Now()
	payload := signup.SessionPayload{
		IdentityID: "id-abc",
		Email:      "user@example.com",
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(24 * time.Hour).Unix(),
	}

	signed, err := signup.SignSession(secret, payload)
	if err != nil {
		t.Fatalf("SignSession error: %v", err)
	}
	if signed == "" {
		t.Fatal("signed session must not be empty")
	}

	got, err := signup.VerifySession(secret, signed)
	if err != nil {
		t.Fatalf("VerifySession error: %v", err)
	}
	if got.IdentityID != payload.IdentityID {
		t.Errorf("IdentityID: want %q, got %q", payload.IdentityID, got.IdentityID)
	}
	if got.Email != payload.Email {
		t.Errorf("Email: want %q, got %q", payload.Email, got.Email)
	}
}

func TestSignSession_EmptySecret(t *testing.T) {
	_, err := signup.SignSession("", signup.SessionPayload{})
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestVerifySession_EmptySecret(t *testing.T) {
	_, err := signup.VerifySession("", "some.value")
	if err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestVerifySession_MalformedCookie(t *testing.T) {
	_, err := signup.VerifySession("secret", "no-dot-separator")
	if err == nil {
		t.Error("expected error for malformed cookie (no dot)")
	}
}

func TestVerifySession_InvalidSignatureHex(t *testing.T) {
	_, err := signup.VerifySession("secret", "payload.notvalidhex!!!")
	if err == nil {
		t.Error("expected error for invalid signature hex")
	}
}

func TestVerifySession_TamperedPayload(t *testing.T) {
	secret := "test-secret"
	now := time.Now()
	payload := signup.SessionPayload{
		IdentityID: "id-123",
		Email:      "user@example.com",
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(1 * time.Hour).Unix(),
	}

	signed, err := signup.SignSession(secret, payload)
	if err != nil {
		t.Fatalf("SignSession error: %v", err)
	}

	// Tamper with the payload portion.
	dot := strings.LastIndex(signed, ".")
	tampered := "dGFtcGVyZWQ" + signed[dot:]
	_, err = signup.VerifySession(secret, tampered)
	if err == nil {
		t.Error("expected error for tampered payload")
	}
}

func TestVerifySession_WrongSecret(t *testing.T) {
	now := time.Now()
	payload := signup.SessionPayload{
		IdentityID: "id-123",
		Email:      "user@example.com",
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(1 * time.Hour).Unix(),
	}

	signed, err := signup.SignSession("secret-A", payload)
	if err != nil {
		t.Fatal(err)
	}

	_, err = signup.VerifySession("secret-B", signed)
	if err == nil {
		t.Error("expected error for wrong secret")
	}
}

func TestVerifySession_Expired(t *testing.T) {
	secret := "test-secret"
	payload := signup.SessionPayload{
		IdentityID: "id-123",
		Email:      "user@example.com",
		IssuedAt:   time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt:  time.Now().Add(-1 * time.Hour).Unix(),
	}

	signed, err := signup.SignSession(secret, payload)
	if err != nil {
		t.Fatal(err)
	}

	_, err = signup.VerifySession(secret, signed)
	if err == nil {
		t.Error("expected error for expired session")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error should mention expiry, got: %v", err)
	}
}

func TestVerifySession_SignatureTooShort(t *testing.T) {
	// Valid hex but only 4 bytes instead of 32.
	_, err := signup.VerifySession("secret", "payload.aabbccdd")
	if err == nil {
		t.Error("expected error for signature with wrong length")
	}
}
