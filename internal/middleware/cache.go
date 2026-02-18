// Package middleware provides Fiber middleware components for the LLM pricing API.
package middleware

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// routeTTLs maps URL path prefixes to their cache TTL.
// Paths not matched by any prefix are not cached (pass-through).
// Matching is prefix-based so "/v1/models/gpt-4" matches "/v1/models".
var routeTTLs = []struct {
	prefix string
	ttl    time.Duration
}{
	{"/v1/context", 10 * time.Minute},
	{"/v1/models", 5 * time.Minute},
	{"/v1/providers", 5 * time.Minute},
	{"/v1/changes", 5 * time.Minute},
	{"/v1/compare", 2 * time.Minute},
}

// ttlForPath returns the cache TTL for the given request path, and whether the
// path is cacheable at all. Only GET requests should be cached; the caller is
// responsible for method gating.
func ttlForPath(path string) (time.Duration, bool) {
	for _, r := range routeTTLs {
		if strings.HasPrefix(path, r.prefix) {
			return r.ttl, true
		}
	}
	return 0, false
}

// cacheKey builds a deterministic Redis key from the request method, path,
// sorted query string, and API tier extracted from the Fiber locals.
//
// Format: cache:{method}:{path}:{sorted_query_string}:{tier}
func cacheKey(c *fiber.Ctx) string {
	method := strings.ToUpper(c.Method())
	path := c.Path()

	// Sort query parameters for cache-key stability regardless of insertion order.
	rawQuery := string(c.Request().URI().QueryString())
	sortedQuery := sortQueryString(rawQuery)

	tier, _ := c.Locals("tier").(string)

	return fmt.Sprintf("cache:%s:%s:%s:%s", method, path, sortedQuery, tier)
}

// sortQueryString parses a raw query string and returns its parameters in
// alphabetical key order, then alphabetical value order for duplicate keys.
func sortQueryString(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
		sort.Strings(values[k])
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		for _, v := range values[k] {
			if v == "" {
				parts = append(parts, url.QueryEscape(k))
			} else {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
	}
	return strings.Join(parts, "&")
}

// Cache returns a Fiber middleware that caches GET responses in Redis.
// Only routes listed in routeTTLs are cached; all others pass through.
//
// Cached responses include a Cache-Control: max-age=N, public header.
// Uncached responses receive Cache-Control: no-store.
//
// The middleware stores the raw response body in Redis as a plain string.
// The Content-Type of cached responses is always application/json; if a
// route returns a different content type it should be excluded from caching.
func Cache(client *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Only cache GET requests.
		if c.Method() != fiber.MethodGet {
			c.Set(fiber.HeaderCacheControl, "no-store")
			return c.Next()
		}

		ttl, cacheable := ttlForPath(c.Path())
		if !cacheable {
			c.Set(fiber.HeaderCacheControl, "no-store")
			return c.Next()
		}

		key := cacheKey(c)
		ctx := context.Background()

		// Attempt cache hit.
		cached, err := client.Get(ctx, key).Bytes()
		if err == nil {
			// Cache hit: return cached body directly.
			maxAge := int(ttl.Seconds())
			c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
			c.Set(fiber.HeaderCacheControl, fmt.Sprintf("max-age=%d, public", maxAge))
			return c.Send(cached)
		}
		if err != redis.Nil {
			// Redis error — log and fall through to origin (fail open).
			// We do not fail the request over a cache miss.
			_ = err
		}

		// Cache miss: call next handler and capture the response.
		if err := c.Next(); err != nil {
			return err
		}

		// Only cache successful JSON responses.
		status := c.Response().StatusCode()
		if status >= 200 && status < 300 {
			body := c.Response().Body()
			if len(body) > 0 {
				// Best-effort store; ignore Redis errors so that a cache write
				// failure does not degrade the API response.
				_ = client.Set(ctx, key, body, ttl).Err()
			}
		}

		// Set Cache-Control on the fresh response.
		maxAge := int(ttl.Seconds())
		c.Set(fiber.HeaderCacheControl, fmt.Sprintf("max-age=%d, public", maxAge))
		return nil
	}
}
