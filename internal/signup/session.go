package signup

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Session represents a verified signup session stored as a signed cookie.
// The cookie value is: base64url(<identityID>:<expiresUnix>).<hmac-sha256>
type Session struct {
	IdentityID string
	ExpiresAt  time.Time
}

// EncodeSession returns a signed cookie value for the given session.
func EncodeSession(s Session, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("signup.EncodeSession: secret must not be empty")
	}
	payload := fmt.Sprintf("%s:%d", s.IdentityID, s.ExpiresAt.Unix())
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	sig := hex.EncodeToString(mac.Sum(nil))

	return encoded + "." + sig, nil
}

// DecodeSession parses and validates a signed cookie value.
// Returns ErrSessionInvalid if the signature is bad, or ErrSessionExpired if
// the session has passed its TTL.
func DecodeSession(value, secret string) (*Session, error) {
	if secret == "" {
		return nil, fmt.Errorf("signup.DecodeSession: secret must not be empty")
	}
	idx := strings.LastIndex(value, ".")
	if idx <= 0 {
		return nil, ErrSessionInvalid
	}
	encoded := value[:idx]
	sig := value[idx+1:]
	if len(sig) != 64 {
		return nil, ErrSessionInvalid
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return nil, ErrSessionInvalid
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrSessionInvalid
	}

	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return nil, ErrSessionInvalid
	}

	var unixTs int64
	if _, err := fmt.Sscanf(parts[1], "%d", &unixTs); err != nil {
		return nil, ErrSessionInvalid
	}

	s := &Session{
		IdentityID: parts[0],
		ExpiresAt:  time.Unix(unixTs, 0),
	}
	if time.Now().After(s.ExpiresAt) {
		return nil, ErrSessionExpired
	}
	return s, nil
}

// ErrSessionInvalid indicates a bad or tampered session cookie.
var ErrSessionInvalid = fmt.Errorf("signup: invalid session")

// ErrSessionExpired indicates the session cookie has passed its TTL.
var ErrSessionExpired = fmt.Errorf("signup: session expired")
