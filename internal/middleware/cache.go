// Package middleware provides Fiber middleware components for the LLM pricing API.
package middleware

import (
	"encoding/json"
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
	{"/v1/context", 1 * time.Hour},
	{"/v1/models", 30 * time.Minute},
	{"/v1/providers", 30 * time.Minute},
	{"/v1/changes", 10 * time.Minute},
	{"/v1/compare", 10 * time.Minute},
}

// replayedHeaders are the response headers stored with a cached body and
// replayed on a hit. List endpoints report totals and cursors here; dropping
// them on a hit breaks clients that page through results.
var replayedHeaders = []string{"X-Total-Count", "X-Has-More", "X-Next-Cursor"}

// cacheEntryVersion identifies the stored format. Entries without it (the
// original raw-body format) are treated as misses and overwritten.
const cacheEntryVersion = 2

type cacheEntry struct {
	V           int               `json:"v"`
	ContentType string            `json:"ct"`
	Headers     map[string]string `json:"h,omitempty"`
	Body        []byte            `json:"b"`
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
// The middleware stores a JSON envelope in Redis holding the response body, its
// Content-Type and the replayedHeaders, and restores all three on a hit.
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
		ctx := c.UserContext()

		// Attempt cache hit.
		cached, err := client.Get(ctx, key).Bytes()
		if err == nil {
			var entry cacheEntry
			if json.Unmarshal(cached, &entry) == nil && entry.V == cacheEntryVersion {
				for name, value := range entry.Headers {
					c.Set(name, value)
				}
				maxAge := int(ttl.Seconds())
				c.Set(fiber.HeaderContentType, entry.ContentType)
				c.Set(fiber.HeaderCacheControl, fmt.Sprintf("max-age=%d, public", maxAge))
				return c.Send(entry.Body)
			}
			// Legacy or unreadable entry: fall through and refresh it.
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
				entry := cacheEntry{
					V:           cacheEntryVersion,
					ContentType: string(c.Response().Header.ContentType()),
					Body:        body,
				}
				for _, name := range replayedHeaders {
					if v := c.Response().Header.Peek(name); len(v) > 0 {
						if entry.Headers == nil {
							entry.Headers = make(map[string]string, len(replayedHeaders))
						}
						entry.Headers[name] = string(v)
					}
				}
				// Best-effort store; ignore marshal and Redis errors so that a
				// cache write failure does not degrade the API response.
				if raw, err := json.Marshal(entry); err == nil {
					_ = client.Set(ctx, key, string(raw), ttl).Err()
				}
			}
		}

		// Set Cache-Control on the fresh response.
		maxAge := int(ttl.Seconds())
		c.Set(fiber.HeaderCacheControl, fmt.Sprintf("max-age=%d, public", maxAge))
		return nil
	}
}
