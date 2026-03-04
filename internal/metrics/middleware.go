package metrics

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
)

// PrometheusMiddleware returns a Fiber middleware that records
// llm_api_requests_total and llm_api_request_duration_seconds for every
// request that passes through the chain.
//
// It reads tier and key_hash from Fiber locals populated by the Auth
// middleware; both default to empty strings for unauthenticated routes
// (health, discovery, metrics).
//
// Design notes:
//   - The path label uses c.Route().Path (the registered pattern, e.g.
//     "/v1/models/:id") rather than c.Path() to avoid high-cardinality
//     labels from dynamic path segments.
//   - The status code is derived post-handler to capture error responses
//     written by the ErrorHandler.
func PrometheusMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		elapsed := time.Since(start).Seconds()

		// Resolve status code: when an error is present the ErrorHandler has
		// not yet written the response body, so read the intended status from
		// the error value directly.
		statusCode := c.Response().StatusCode()
		if err != nil {
			switch e := err.(type) {
			case *api.ProblemDetail:
				statusCode = e.Status
			case *fiber.Error:
				statusCode = e.Code
			default:
				statusCode = fiber.StatusInternalServerError
			}
		}

		status := strconv.Itoa(statusCode)
		// Use the registered route pattern to avoid per-ID cardinality explosion.
		path := c.Route().Path
		method := c.Method()
		// Keep keys in sync with Auth middleware locals keys.
		const (
			localKeyTier = "tier"
			localKeyHash = "key_hash"
		)
		tier, _ := c.Locals(localKeyTier).(string)
		hash, _ := c.Locals(localKeyHash).(string)

		RequestsTotal.WithLabelValues(method, path, status, tier, hash).Inc()
		ObserveActiveKey(tier, hash)
		RequestDurationSeconds.WithLabelValues(method, path).Observe(elapsed)

		// Count 5xx as errors.
		if statusCode >= 500 {
			errType := "internal_error"
			if err != nil {
				errType = errorType(err)
			}
			ErrorsTotal.WithLabelValues(method, path, errType).Inc()
		}

		return err
	}
}

// errorType derives a concise error_type label value from a handler error.
func errorType(err error) string {
	switch err.(type) {
	case *api.ProblemDetail:
		return "problem_detail"
	case *fiber.Error:
		return "fiber_error"
	default:
		return "unknown"
	}
}
