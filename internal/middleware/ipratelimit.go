package middleware

import (
	"crypto/sha256"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/api"
)

const (
	// ipRateLimitWindow is the fixed window for IP-based rate limiting.
	ipRateLimitWindow = 15 * time.Minute
	// ipRateLimitMax is the maximum number of requests per IP in the window.
	ipRateLimitMax = 10
)

// IPRateLimit returns a Fiber middleware that enforces per-IP rate limiting
// using a Redis counter with a fixed window. Designed for public endpoints
// (e.g. auth routes) that are not protected by Unkey API key auth.
func IPRateLimit(redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		now := time.Now().Unix()
		windowSec := int64(ipRateLimitWindow.Seconds())
		ip := realIP(c)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(ip)))
		windowIndex := now / windowSec
		windowKey := fmt.Sprintf("iprl:%s:%d", hash[:16], windowIndex)

		count, err := redisClient.Incr(c.Context(), windowKey).Result()
		if err != nil {
			// Redis hiccup: let the request through.
			return c.Next()
		}

		if count == 1 {
			if expErr := redisClient.Expire(c.Context(), windowKey, ipRateLimitWindow).Err(); expErr != nil {
				// Key has no TTL — will leak. Log but don't block the request.
				log.Printf("ipratelimit: Expire failed for key %s: %v", windowKey, expErr)
			}
		}

		if count > ipRateLimitMax {
			windowEnd := time.Unix((windowIndex+1)*windowSec, 0)
			retryAfter := int(time.Until(windowEnd).Seconds())
			if retryAfter < 0 {
				retryAfter = 0
			}
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			return api.NewTooManyRequests("too many requests — try again later")
		}

		return c.Next()
	}
}
