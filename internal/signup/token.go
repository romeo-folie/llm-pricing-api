package signup

// token.go — magic-link token generation and session signing for the
// internal/auth flow, the only signup HTTP layer that is mounted.
//
//   - GenerateRawToken / BuildVerifyURL: the emailed one-time token and its URL.
//     Only the raw token is emailed; store.go persists HashToken(raw).
//   - SignSession / VerifySession: the session cookie, formatted as
//     base64url(JSON).<hmac-sha256>, embedding email + issued-at + expires-at so
//     the auth handler avoids a DB round-trip on every authenticated request.
//
// The payload is signed, not encrypted — never put a secret in it.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
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
	return BuildVerifyURLWithNext(baseURL, path, rawToken, "")
}

// BuildVerifyURLWithNext is BuildVerifyURL plus an optional post-verification
// redirect target — used by the agent flow so an approval survives the email
// round trip (e.g. next=/activate?code=XXXX).
//
// next is re-validated here with SafeNextPath rather than trusted by the
// caller: an unsafe value would turn the emailed link into an open redirect,
// and an emailed link is the least convenient place for a user to notice one.
func BuildVerifyURLWithNext(baseURL, path, rawToken, next string) string {
	safe := SafeNextPath(next)

	u, err := url.Parse(baseURL)
	if err != nil {
		// Fallback: should not happen with validated config.
		out := baseURL + path + "?token=" + url.QueryEscape(rawToken)
		if safe != "" {
			out += "&next=" + url.QueryEscape(safe)
		}
		return out
	}
	if joined, err := url.JoinPath(u.Path, path); err == nil {
		u.Path = joined
	} else {
		// Fallback: simple concatenation (same as non-parseable base URL path above).
		u.Path = u.Path + path
	}
	q := u.Query()
	q.Set("token", rawToken)
	if safe != "" {
		q.Set("next", safe)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// maxNextPathLen bounds a caller-supplied post-verification redirect target.
const maxNextPathLen = 512

// SafeNextPath validates a caller-supplied redirect target and returns it when
// it is safe to use, or "" when it is not.
//
// Only same-origin absolute paths are accepted. The value arrives in a public
// signup request and becomes the Location of a 302 after verification, so an
// unchecked value is an open redirect: a trusted llmrates.live URL would bounce
// the user to an attacker's site. Rejected forms include absolute URLs
// ("https://evil"), protocol-relative URLs ("//evil"), and backslash variants
// that some clients normalise into "//" ("/\evil").
//
// A device-approval `code` parameter is stripped from the path. This is the
// difference between "return the user to the page they were on" and "let an
// emailed link pre-load someone else's approval screen": request-link is
// unauthenticated and mails an arbitrary address, so if `next` could carry a
// code, an attacker could send a victim a genuine llmrates.live email that opens
// the approval screen for the attacker's grant, defeating the whole point of
// showing the code for the user to compare against their own terminal. The
// approval page restores a code it stashed locally instead, so the legitimate
// round trip still works.
func SafeNextPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxNextPathLen {
		return ""
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	// Backslashes and control characters anywhere: some clients treat "\" as
	// "/", and a CR/LF here would be header injection if it ever reached one.
	if strings.ContainsAny(raw, "\\\r\n\t") {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	// Match the parameter case-insensitively. The API reads `code` exactly, so an
	// upper-case variant would not pre-load a grant today, but stripping it too
	// costs nothing and does not depend on every future reader being
	// case-sensitive. RawQuery is only rewritten when something was removed, so
	// an unrelated query string is passed through byte-for-byte.
	q := u.Query()
	stripped := false
	for key := range q {
		if strings.EqualFold(key, "code") {
			q.Del(key)
			stripped = true
		}
	}
	if stripped {
		u.RawQuery = q.Encode()
	}
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
