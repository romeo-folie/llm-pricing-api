package signup

// This file contains session signing helpers for the magic-link auth flow.
// Token hashing (for storage) is handled by HashToken in store.go.
// Session signing uses HMAC-SHA256 so the cookie is self-verifying.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"
)

// GenerateRawToken returns a cryptographically random 32-byte token encoded
// as a URL-safe base64 string. This value is emailed to the user; only its
// SHA-256 hash (HashToken) is stored.
func GenerateRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signup: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BuildVerifyURL constructs the full magic-link URL from config values and
// the raw (un-hashed) token. Uses net/url for safe encoding.
func BuildVerifyURL(baseURL, path, rawToken string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		// Fallback: should not happen with validated config.
		return baseURL + path + "?token=" + url.QueryEscape(rawToken)
	}
	u.Path, _ = url.JoinPath(u.Path, path)
	q := u.Query()
	q.Set("token", rawToken)
	u.RawQuery = q.Encode()
	return u.String()
}

// ── Session cookie ────────────────────────────────────────────────────────────

// SessionPayload is the data embedded in the signed session cookie.
type SessionPayload struct {
	IdentityID string `json:"identity_id"`
	Email      string `json:"email"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
}

// SignSession encodes a SessionPayload as base64(JSON) and appends an
// HMAC-SHA256 signature: "<payload>.<sig>".
// The cookie value is self-contained — no server-side session store needed.
func SignSession(secret string, p SessionPayload) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("signup: session: signing secret must not be empty")
	}
	data, err := encodePayload(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	sig := hex.EncodeToString(mac.Sum(nil))
	return data + "." + sig, nil
}

// VerifySession parses and validates a signed session cookie value.
// Returns the payload and nil on success; an error on tampering or expiry.
func VerifySession(secret, cookieValue string) (SessionPayload, error) {
	if len(secret) == 0 {
		return SessionPayload{}, fmt.Errorf("signup: session: signing secret must not be empty")
	}
	dot := lastDot(cookieValue)
	if dot < 0 {
		return SessionPayload{}, fmt.Errorf("signup: session: malformed cookie")
	}
	data, sigHex := cookieValue[:dot], cookieValue[dot+1:]

	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != 32 {
		return SessionPayload{}, fmt.Errorf("signup: session: invalid signature format")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return SessionPayload{}, fmt.Errorf("signup: session: invalid signature")
	}

	var p SessionPayload
	if err := decodePayload(data, &p); err != nil {
		return SessionPayload{}, fmt.Errorf("signup: session: decode: %w", err)
	}
	if time.Now().Unix() >= p.ExpiresAt {
		return SessionPayload{}, fmt.Errorf("signup: session: expired")
	}
	return p, nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func encodePayload(p SessionPayload) (string, error) {
	b, err := defaultCodec.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodePayload(encoded string, p *SessionPayload) error {
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	return defaultCodec.Unmarshal(b, p)
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}
