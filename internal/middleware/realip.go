package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// RealIP returns the client IP, optionally honouring X-Forwarded-For when the
// immediate peer (c.IP()) is in the trustedProxies list.
//
// Behaviour:
//   - trustedProxies non-empty: if c.IP() matches any entry, return the
//     leftmost X-Forwarded-For value (the original client IP). Otherwise
//     return c.IP().
//   - trustedProxies empty (default): return c.IP() only — XFF is never
//     consulted. This is the conservative default that prevents spoofing when
//     the service is reachable without a trusted reverse proxy.
//
// In production the Fiber app is configured with ProxyHeader + TrustedProxies,
// so c.IP() already returns the correct client IP and trustedProxies can be
// left empty.
func RealIP(c *fiber.Ctx, trustedProxies ...string) string {
	if len(trustedProxies) == 0 {
		return c.IP()
	}

	peerIP := c.IP()
	if !isPeerTrusted(peerIP, trustedProxies) {
		return peerIP
	}

	if xff := c.Get("X-Forwarded-For"); xff != "" {
		// Use the leftmost (first) entry — this is the original client IP.
		// Rightmost is the last proxy hop, not the client.
		if idx := strings.Index(xff, ","); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	return peerIP
}

// isPeerTrusted returns true if peerIP matches any entry in the trusted list.
func isPeerTrusted(peerIP string, trusted []string) bool {
	for _, t := range trusted {
		if t == peerIP {
			return true
		}
	}
	return false
}
