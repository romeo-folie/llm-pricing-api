package middleware

import "github.com/gofiber/fiber/v2"

// Security returns a Fiber middleware that sets defensive HTTP security headers
// on every response. Headers applied:
//
//   - X-Content-Type-Options: nosniff  — prevents MIME-type sniffing attacks.
//   - X-Frame-Options: DENY            — prevents clickjacking via iframes.
func Security() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		return c.Next()
	}
}
