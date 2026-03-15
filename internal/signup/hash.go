package signup

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashValue returns the SHA-256 hex hash of s.
// Used for privacy-safe storage of IP addresses and user-agent strings.
func hashValue(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
