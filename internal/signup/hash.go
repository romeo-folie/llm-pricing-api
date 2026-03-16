package signup

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// hashValue returns the HMAC-SHA256 hex hash of s keyed with secret.
// Used for privacy-safe storage of IP addresses and user-agent strings.
// Plain SHA-256 is insufficient for low-entropy values (IPs, common UAs)
// because they are enumerable via brute-force; a server secret prevents
// offline reversal of the stored hashes.
func hashValue(s, secret string) string {
	if s == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(s))
	return hex.EncodeToString(mac.Sum(nil))
}
