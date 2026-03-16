package signup

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
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
//   - rawToken: the raw random token, used only for hashing/storage (never sent to user directly)
//   - tokenHash: SHA-256 of rawToken, stored in the DB for lookup
//   - expiresAt: absolute expiry timestamp
//   - magicLinkURL: the full verification URL to email the user;
//     the query param `token` contains the signed token (rawToken + "." + HMAC-hex)
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
	tokenHash = HashToken(rawToken)

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
	// Split on the last '.' to separate payload from HMAC-SHA256 hex signature.
	idx := strings.LastIndex(signed, ".")
	if idx <= 0 {
		return "", "", fmt.Errorf("signup.ParseToken: malformed token")
	}
	raw := signed[:idx]
	sig := signed[idx+1:]
	if len(sig) != 64 { // HMAC-SHA256 hex = 64 chars
		return "", "", fmt.Errorf("signup.ParseToken: malformed token signature length")
	}

	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(raw))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", "", fmt.Errorf("signup.ParseToken: invalid token signature")
	}

	return raw, HashToken(raw), nil
}
