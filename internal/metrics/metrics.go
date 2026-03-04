// Package metrics registers all Prometheus metrics for the llm-pricing-api
// service. Each metric is declared as a package-level variable so it can be
// incremented from any package without creating registration cycles.
//
// All metrics are registered on the default prometheus.DefaultRegisterer so
// the standard promhttp.Handler() can serve them at /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ── HTTP API ──────────────────────────────────────────────────────────────

	// RequestsTotal counts every completed HTTP request.
	// Labels: method, path, status (HTTP status code string), tier, key_hash.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_api_requests_total",
		Help: "Total number of HTTP requests handled, partitioned by method, path, status, tier, and key_hash.",
	}, []string{"method", "path", "status", "tier", "key_hash"})

	// RequestDurationSeconds observes per-request latency.
	// Labels: method, path.
	RequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_api_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets, // .005 .01 .025 .05 .1 .25 .5 1 2.5 5 10
	}, []string{"method", "path"})

	// RateLimitHitsTotal counts requests rejected by the rate limiter (HTTP 429).
	// Labels: tier, key_hash.
	RateLimitHitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_api_rate_limit_hits_total",
		Help: "Total number of requests rejected by the rate limiter.",
	}, []string{"tier", "key_hash"})

	// ActiveKeys tracks the number of distinct API keys seen in the last rolling
	// hour. Partitioned by tier.
	// Labels: tier.
	ActiveKeys = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_api_active_keys",
		Help: "Number of distinct API keys seen in the last hour, partitioned by tier.",
	}, []string{"tier"})

	// ErrorsTotal counts API-level errors (5xx responses).
	// Labels: method, path, error_type.
	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_api_errors_total",
		Help: "Total number of API errors, partitioned by method, path, and error type.",
	}, []string{"method", "path", "error_type"})

	// ── Data pipeline ─────────────────────────────────────────────────────────

	// ScraperRunsTotal counts scraper execution attempts.
	// Labels: source (openrouter, litellm, huggingface), status (success, error).
	ScraperRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_scraper_runs_total",
		Help: "Total number of scraper runs, partitioned by source and status.",
	}, []string{"source", "status"})

	// ReconcilerEventsTotal counts price reconciliation events processed.
	// Labels: event_type (price_change, no_change, new_model, model_removed).
	ReconcilerEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_reconciler_events_total",
		Help: "Total number of reconciler events processed, partitioned by event type.",
	}, []string{"event_type"})

	// WebhookDeliveriesTotal counts webhook delivery attempts.
	// Labels: status (success, error, retry).
	WebhookDeliveriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_webhook_deliveries_total",
		Help: "Total number of webhook delivery attempts, partitioned by status.",
	}, []string{"status"})
)
