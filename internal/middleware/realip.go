package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// realIP returns the client IP from X-Forwarded-For (first hop) or c.IP() fallback.
// It trusts X-Forwarded-For unconditionally; this is safe only when the app
// runs behind a trusted reverse proxy (e.g. Railway, Fly, nginx). Do not
// expose this app directly to the internet without a proxy or configure
// Fiber's ProxyHeader/TrustedProxies to restrict which peers can set this header.
func realIP(c *fiber.Ctx) string {
	if ip := c.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.Index(ip, ","); idx >= 0 {
			return strings.TrimSpace(ip[:idx])
		}
		return strings.TrimSpace(ip)
	}
	return c.IP()
}
