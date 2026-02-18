package middleware

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/api"
)

const (
	// rateLimitTTL is the Redis TTL for rate-limit counters (25 hours so the
	// key always outlives the calendar day it was created for).
	rateLimitTTL = 25 * time.Hour

	// Free and developer daily limits.
	rateLimitFree      = 100
	rateLimitDeveloper = 10_000
)

// RateLimit returns a Fiber middleware that enforces per-key, per-calendar-day
// (UTC) request limits based on the tier stored by the Auth middleware.
//
// Counter key pattern: ratelimit:{sha256(raw_key)}:{YYYY-MM-DD}
// The SHA-256 key hash is read from c.Locals("key_hash") so the raw API key
// is never stored in Redis.
//
//   - Pro: unlimited — no counter is touched.
//   - Developer: 10,000 req/day
//   - Free (default): 100 req/day
//
// Exceeding the limit returns HTTP 429 with a Retry-After header set to the
// number of seconds until midnight UTC.
func RateLimit(redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tier, _ := c.Locals(LocalKeyTier).(string)
		hash, _ := c.Locals(LocalKeyHash).(string)

		// Pro tier: unlimited, skip counting entirely.
		if tier == TierPro {
			return c.Next()
		}

		// Determine the daily limit for this tier.
		limit := rateLimitFree
		if tier == TierDeveloper {
			limit = rateLimitDeveloper
		}

		// Build a UTC date-scoped counter key.
		now := time.Now().UTC()
		dateStr := now.Format("2006-01-02")
		counterKey := fmt.Sprintf("ratelimit:%s:%s", hash, dateStr)
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

		// Atomically increment and return the new count.
		count, err := redisClient.Incr(c.Context(), counterKey).Result()
		if err != nil {
			// Redis hiccup: let the request through rather than blocking all
			// traffic, but do not count it.
			return c.Next()
		}

		// Set TTL only on the first write (count == 1) to avoid resetting the
		// window on every request. If ExpireAt fails we still served the count.
		if count == 1 {
			// Expire at the start of the next UTC day so the key lives a full day.
			// Use rateLimitTTL as a safety net (25h) in case ExpireAt fails.
			_ = redisClient.ExpireAt(c.Context(), counterKey, midnight).Err()
			// Belt-and-suspenders: also set a 25h TTL via Expire.
			_ = redisClient.Expire(c.Context(), counterKey, rateLimitTTL).Err()
		}

		if int(count) > limit {
			// Compute seconds until midnight UTC for Retry-After.
			retryAfter := int(time.Until(midnight).Seconds())
			if retryAfter < 0 {
				retryAfter = 0
			}

			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			pd := api.NewTooManyRequests(
				fmt.Sprintf("daily limit of %d requests exceeded; resets at midnight UTC", limit),
			)
			return pd
		}

		return c.Next()
	}
}
