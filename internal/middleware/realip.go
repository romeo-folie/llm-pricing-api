package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// realIP returns the client IP from X-Forwarded-For (first hop) or c.IP() fallback.
func realIP(c *fiber.Ctx) string {
	if ip := c.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.Index(ip, ","); idx >= 0 {
			return strings.TrimSpace(ip[:idx])
		}
		return strings.TrimSpace(ip)
	}
	return c.IP()
}
