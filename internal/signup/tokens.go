package signup

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// TokenConfig holds the parameters for magic-link token generation.
type TokenConfig struct {
	// SigningSecret is used to HMAC-sign the raw token.
	SigningSecret string
	// TTL is the token lifetime. Default: 15 minutes.
	TTL time.Duration
	// BaseURL is the site base URL (e.g. https://llmrates.live).
	BaseURL string
	// Path is the verification route (e.g. /signup/verify).
	Path string
}

// GenerateToken creates a cryptographically random raw token (32 bytes,
// base64url encoded), signs it with HMAC-SHA256, and returns:
//   - rawToken: sent to the user in the magic-link URL (query param `token`)
//   - tokenHash: SHA-256 of rawToken, stored in the DB
//   - expiresAt: absolute expiry timestamp
//   - magicLinkURL: the full verification URL to email the user
func GenerateToken(cfg TokenConfig) (rawToken, tokenHash, magicLinkURL string, expiresAt time.Time, err error) {
	if cfg.SigningSecret == "" {
		err = fmt.Errorf("signup.GenerateToken: SigningSecret must not be empty")
		return
	}
	// 32 random bytes → 43-char base64url string (no padding).
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return
	}
	rawToken = base64.RawURLEncoding.EncodeToString(raw)

	// HMAC-SHA256 signature over the raw token using the signing secret.
	// The signature is appended as .<hex> so the verifier can authenticate
	// the token before hitting the DB (prevents timing oracle attacks).
	mac := hmac.New(sha256.New, []byte(cfg.SigningSecret))
	_, _ = mac.Write([]byte(rawToken))
	sig := hex.EncodeToString(mac.Sum(nil))
	signed := rawToken + "." + sig

	// SHA-256 of the raw token (without the signature) is what we store.
	h := sha256.Sum256([]byte(rawToken))
	tokenHash = hex.EncodeToString(h[:])

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	expiresAt = time.Now().Add(ttl)

	magicLinkURL = fmt.Sprintf("%s%s?token=%s", cfg.BaseURL, cfg.Path, signed)
	return
}

// ParseToken extracts and validates the raw token from a signed value,
// then returns the raw token and its SHA-256 hash for DB lookup.
// Returns an error when the signature does not match.
func ParseToken(signed, signingSecret string) (rawToken, tokenHash string, err error) {
	if signingSecret == "" {
		return "", "", fmt.Errorf("signup.ParseToken: signingSecret must not be empty")
	}
	// Split on the last '.' to separate payload from signature.
	idx := len(signed) - 64 - 1 // hex(sha256) = 64 chars, preceded by '.'
	if idx <= 0 || signed[idx] != '.' {
		return "", "", fmt.Errorf("signup: malformed token")
	}
	raw := signed[:idx]
	sig := signed[idx+1:]

	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(raw))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", "", fmt.Errorf("signup: invalid token signature")
	}

	h := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(h[:]), nil
}
