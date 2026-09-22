package middleware

import (
	"crypto/sha256"
	"fmt"
	"strings"
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

// IPRateLimitConfig holds optional configuration for IPRateLimit.
type IPRateLimitConfig struct {
	// TimeNow overrides the clock used for window calculation.
	// Defaults to time.Now when nil. Useful for deterministic tests.
	TimeNow func() time.Time
	// Max overrides the per-window request cap. Zero or negative uses the
	// default ipRateLimitMax.
	//
	// Callers need this because a single limit cannot serve both a human signup
	// form and an agent polling for approval: the poll loop legitimately issues a
	// request every few seconds for the whole grant lifetime, which exhausts a
	// signup-shaped bucket in under a minute.
	Max int
	// Bucket namespaces this limiter's Redis counters. Two limiters that share a
	// counter increment each other's, halving both effective budgets, so distinct
	// limiters must pass distinct buckets. Defaults to "default".
	Bucket string
	// SkipPrefixes lists path prefixes this limiter must not count.
	//
	// It is required whenever a broader group mount would otherwise capture a
	// nested group that has its own limit: Fiber's Group/Use match by prefix, so
	// a limiter mounted at /auth also runs for /auth/agent. Without a skip, the
	// poll-scale limit on the nested group is silently capped by the broader one.
	SkipPrefixes []string
}

// IPRateLimit returns a Fiber middleware that enforces per-IP rate limiting
// using a Redis counter with a fixed window. Designed for public endpoints
// (e.g. auth routes) that are not protected by Unkey API key auth.
//
// The optional trustedProxies list is forwarded to RealIP to control whether
// X-Forwarded-For is honoured. When empty (default) RealIP returns c.IP()
// only, which is safe when Fiber's app-level ProxyHeader config handles
// proxy trust.
func IPRateLimit(redisClient *redis.Client, fallback zerolog.Logger, trustedProxies ...string) fiber.Handler {
	return IPRateLimitWithConfig(redisClient, fallback, IPRateLimitConfig{}, trustedProxies...)
}

// IPRateLimitWithConfig is like IPRateLimit but accepts an IPRateLimitConfig
// for overriding the clock (useful in tests).
func IPRateLimitWithConfig(redisClient *redis.Client, fallback zerolog.Logger, cfg IPRateLimitConfig, trustedProxies ...string) fiber.Handler {
	timeNow := cfg.TimeNow
	if timeNow == nil {
		timeNow = time.Now
	}
	limit := cfg.Max
	if limit <= 0 {
		limit = ipRateLimitMax
	}
	bucket := cfg.Bucket
	if bucket == "" {
		bucket = "default"
	}

	return func(c *fiber.Ctx) error {
		// Paths owned by a more specific limiter are not counted here at all.
		for _, prefix := range cfg.SkipPrefixes {
			if strings.HasPrefix(c.Path(), prefix) {
				return c.Next()
			}
		}

		nowTime := timeNow()
		now := nowTime.Unix()
		windowSec := int64(ipRateLimitWindow.Seconds())
		ip := RealIP(c, trustedProxies...)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(ip)))
		windowIndex := now / windowSec
		windowKey := fmt.Sprintf("iprl:%s:%s:%d", bucket, hash[:16], windowIndex)
		windowEnd := time.Unix((windowIndex+1)*windowSec, 0)

		count, err := redisClient.Incr(c.UserContext(), windowKey).Result()
		if err != nil {
			// Redis hiccup: let the request through.
			return c.Next()
		}

		if count == 1 {
			// Use ExpireAt to align the key lifetime with the fixed window boundary,
			// rather than Expire which could keep keys around for almost an extra window.
			if expErr := redisClient.ExpireAt(c.UserContext(), windowKey, windowEnd).Err(); expErr != nil {
				// Key has no TTL — will leak. Log but don't block the request.
				l := logger.FromContext(c.UserContext(), fallback)
				l.Error().Err(expErr).Str("key", windowKey[:8]+"...").Msg("ipratelimit: ExpireAt failed — key may have no TTL")
				// Belt-and-suspenders: fall back to relative TTL.
				_ = redisClient.Expire(c.UserContext(), windowKey, ipRateLimitWindow).Err()
			}
		}

		if count > int64(limit) {
			// Reuse the captured nowTime so Retry-After is consistent with the
			// window calculation above (no second clock read).
			retryAfter := int(windowEnd.Sub(nowTime).Seconds())
			if retryAfter < 0 {
				retryAfter = 0
			}
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			return api.NewTooManyRequests("too many requests — try again later")
		}

		return c.Next()
	}
}
