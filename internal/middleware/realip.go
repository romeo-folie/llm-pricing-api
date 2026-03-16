package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// RealIP returns the client IP from the rightmost entry in X-Forwarded-For,
// or c.IP() as a fallback. Using the rightmost entry (the one appended by
// the last trusted reverse proxy) prevents clients from spoofing their IP
// by injecting values at the start of X-Forwarded-For. This is safe when
// the app runs behind a single trusted reverse proxy (e.g. Railway, Fly,
// nginx). If additional trusted proxies exist in the chain, their IPs
// should be stripped before selecting the rightmost entry.
func RealIP(c *fiber.Ctx) string {
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		// Use the rightmost (last) entry — this is the IP that connected
		// to our trusted proxy, not a client-supplied value.
		if idx := strings.LastIndex(xff, ","); idx >= 0 {
			return strings.TrimSpace(xff[idx+1:])
		}
		return strings.TrimSpace(xff)
	}
	return c.IP()
}
