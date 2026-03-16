package middleware

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/logger"
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
//
// The optional trustedProxies list is forwarded to RealIP to control whether
// X-Forwarded-For is honoured. When empty (default) RealIP returns c.IP()
// only, which is safe when Fiber's app-level ProxyHeader config handles
// proxy trust.
func IPRateLimit(redisClient *redis.Client, fallback zerolog.Logger, trustedProxies ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		now := time.Now().Unix()
		windowSec := int64(ipRateLimitWindow.Seconds())
		ip := RealIP(c, trustedProxies...)
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
				l := logger.FromContext(c.Context(), fallback)
				l.Error().Err(expErr).Str("key", windowKey[:8]+"...").Msg("ipratelimit: Expire failed — key may have no TTL")
				// Belt-and-suspenders: set absolute expiry at window end.
				windowEnd := time.Unix((windowIndex+1)*windowSec, 0)
				_ = redisClient.ExpireAt(c.Context(), windowKey, windowEnd).Err()
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
