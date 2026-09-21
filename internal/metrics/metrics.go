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
	// Labels: method, path, status (HTTP status code string), tier.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_api_requests_total",
		Help: "Total number of HTTP requests handled, partitioned by method, path, status, and tier.",
	}, []string{"method", "path", "status", "tier"})

	// RequestDurationSeconds observes per-request latency.
	// Labels: method, path.
	RequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_api_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets, // .005 .01 .025 .05 .1 .25 .5 1 2.5 5 10
	}, []string{"method", "path"})

	// RateLimitHitsTotal counts requests rejected by the rate limiter (HTTP 429).
	//
	// Labelled by tier only. It previously carried key_hash too, which emitted
	// one series per API key — and every signup, including every skill user,
	// creates a key, so cardinality grew without bound (#198). Per-key detail
	// is still available where it belongs: the Redis counter
	// ratelimit:{sha256(key)}:{date} and the request logs.
	// Labels: tier.
	RateLimitHitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_api_rate_limit_hits_total",
		Help: "Total number of requests rejected by the rate limiter, partitioned by tier.",
	}, []string{"tier"})

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
	// Labels: event_type. Emitted values (see internal/reconciler): no_change,
	// discrepancy_flagged, pending_change_seen, first_seen_published,
	// price_published.
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

	// BenchmarkScrapeRunsTotal counts benchmark leaderboard scrape attempts.
	//
	// It is deliberately separate from ScraperRunsTotal: the price scrapers
	// funnel through worker.runPipeline (which owns that counter), while the
	// benchmark scrapers do not — they fetch a leaderboard and then trigger a
	// capability recompute, with no diff/reconcile stage. Folding them into one
	// counter would give the price-scrape failure alert a source it can never
	// fix by re-running a price scrape.
	//
	// Labels: source (swebench, livecodebench), status (success, error).
	BenchmarkScrapeRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_benchmark_scrape_runs_total",
		Help: "Total number of benchmark leaderboard scrape runs, partitioned by source and status.",
	}, []string{"source", "status"})

	// BenchmarkScrapeDurationSeconds observes per-run benchmark scrape latency.
	// The buckets run to 5 minutes because these scrapers fetch raw leaderboard
	// artifacts (SWE-bench fetches a multi-MB JSON document) rather than a
	// small pricing feed, so prometheus.DefBuckets — which stops at 10s — would
	// collapse every real observation into +Inf.
	// Labels: source.
	BenchmarkScrapeDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_benchmark_scrape_duration_seconds",
		Help:    "Benchmark leaderboard scrape latency in seconds.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300},
	}, []string{"source"})

	// SlugResolutionsTotal counts leaderboard model names that reached
	// slugmap resolution, partitioned by outcome.
	//
	// This is the metric that answers whether thin benchmark coverage is caused
	// by upstream publishing few mappable models or by the resolver rejecting
	// most entries: `unknown` means the allowlist has no row for the name,
	// `ambiguous` means more than one fallback rule matched the same name.
	// Labels: result (resolved, unknown, ambiguous).
	SlugResolutionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_slug_resolutions_total",
		Help: "Total number of leaderboard model-name resolutions attempted, partitioned by outcome.",
	}, []string{"result"})

	// BenchmarkEvidenceActive is the number of active benchmark evidence rows
	// for a benchmark. "Active" mirrors intelligence.GetActiveBenchmarkScores:
	// exactly one normalised row per (model, benchmark), chosen by source
	// observation time, then evaluation time. A benchmark with no active
	// evidence emits no sample (see worker.BenchmarkSampler) so the gauge can be
	// absent rather than reading as a misleading zero.
	// Labels: benchmark.
	BenchmarkEvidenceActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_benchmark_evidence_active",
		Help: "Number of active benchmark evidence rows for a benchmark.",
	}, []string{"benchmark"})

	// BenchmarkEvidenceStaleRatio is the fraction (0–1) of a benchmark's active
	// evidence whose evaluated_at is older than the scoring staleness threshold
	// (90 days).
	//
	// It is measured from evaluated_at, never last_observed_at: SWE-bench is
	// re-scraped daily, so last_observed_at is always fresh, while the newest
	// published evaluation is months old. Measuring observation time would
	// report a healthy benchmark whose evidence the scorer itself treats as
	// stale.
	// Labels: benchmark.
	BenchmarkEvidenceStaleRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_benchmark_evidence_stale_ratio",
		Help: "Fraction of a benchmark's active evidence older than the staleness threshold.",
	}, []string{"benchmark"})

	// CapabilityRecomputeDurationSeconds observes how long a full capability
	// score recomputation takes.
	CapabilityRecomputeDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "llm_capability_recompute_duration_seconds",
		Help:    "Duration of a full capability score recomputation in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// CapabilityRecomputeFailuresTotal counts failed full recomputations. The
	// task that triggered the recompute still fails and retries; this counter is
	// what makes the retry loop visible.
	CapabilityRecomputeFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "llm_capability_recompute_failures_total",
		Help: "Total number of failed full capability score recomputations.",
	})

	// CapabilityLastRecomputeTimestampSeconds is the Unix timestamp of the last
	// successful full capability recomputation. A gauge rather than a monotonic
	// counter so `time() - llm_capability_last_recompute_timestamp_seconds`
	// stays meaningful across process restarts. It is only advanced on success,
	// so a recompute that keeps failing leaves the value receding.
	CapabilityLastRecomputeTimestampSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "llm_capability_last_recompute_timestamp_seconds",
		Help: "Unix timestamp of the last successful full capability score recomputation.",
	})

	// ── Data freshness ────────────────────────────────────────────────────────

	// SourceLastSuccessTimestampSeconds is the Unix timestamp of the last
	// successful verification for a source. It is refreshed by a periodic ticker
	// in the worker (internal/worker.FreshnessSampler), not only when a scrape
	// succeeds: a gauge updated solely inside the scrape pipeline would freeze at
	// its last good value when a scraper stops running, so
	// time() - llm_source_last_success_timestamp_seconds would stay small and the
	// staleness alert would never fire. Sources with no published prices emit no
	// sample.
	// Labels: source.
	SourceLastSuccessTimestampSeconds = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_source_last_success_timestamp_seconds",
		Help: "Unix timestamp of the last successful verification for a source.",
	}, []string{"source"})

	// PricesStaleRatio is the fraction (0–1) of a source's *active* prices whose
	// last_verified_at is older than the staleness threshold. Active means
	// verified inside the activity window, so models upstream has delisted or
	// renamed — which the pipeline will never see again — drop out instead of
	// inflating the ratio forever (#217). Labelled by source rather than by
	// confidence: confidence is computed per API response and is not stored on
	// the prices rows, so it is neither cheap nor actionable here — the source
	// label names the feed that went quiet.
	// Labels: source.
	PricesStaleRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_prices_stale_ratio",
		Help: "Fraction of a source's active published prices older than the staleness threshold.",
	}, []string{"source"})

	// PricesActive is the denominator of llm_prices_stale_ratio: the number of a
	// source's prices verified inside the activity window. Published alongside
	// the ratio so a spike can be read against the population it was computed
	// over, and so the excluded (delisted or orphaned) rows are quantified by
	// the gap between it and llm_prices_published.
	// Labels: source.
	PricesActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_prices_active",
		Help: "Number of a source's prices verified inside the activity window.",
	}, []string{"source"})

	// PricesPublished is the number of published prices for a source. It gives
	// llm_prices_stale_ratio absolute context so a high ratio on a tiny
	// denominator is not mistaken for a broad outage.
	// Labels: source.
	PricesPublished = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_prices_published",
		Help: "Number of published prices for a source.",
	}, []string{"source"})

	// ── Job queue ─────────────────────────────────────────────────────────────

	// QueueTasks is the number of asynq tasks in a queue by state, sampled from
	// the Inspector on a ticker by worker.QueueSampler.
	//
	// The pipeline's other signals say whether work succeeded; this one says
	// whether work is *stuck*, which nothing else could see. Concretely: a
	// failing task holds its asynq Unique(24h) lock while it retries, so the
	// startup enqueue of a fixed scraper is silently deduplicated and the fix
	// appears not to work. Queue depth makes that visible.
	//
	// States are asynq's own: pending, active, scheduled, retry, archived,
	// completed, aggregating. A queue that has never held a task emits no
	// sample rather than zeroes.
	// Labels: queue, state.
	QueueTasks = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_asynq_queue_tasks",
		Help: "Number of asynq tasks in a queue, by state.",
	}, []string{"queue", "state"})

	// QueueLatencySeconds is the age of the oldest pending task in a queue.
	//
	// It is the better backlog signal of the two: a queue holding a handful of
	// tasks that are being worked through is healthy, while a single task
	// waiting an hour is not. Pending counts cannot tell those apart.
	// Labels: queue.
	QueueLatencySeconds = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_asynq_queue_latency_seconds",
		Help: "Age of the oldest pending task in a queue, in seconds.",
	}, []string{"queue"})

	// QueuePaused reports whether a queue is paused (1) or running (0), so a
	// queue that has been paused — by an operator or by a bug — is visible
	// rather than merely quiet.
	// Labels: queue.
	QueuePaused = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_asynq_queue_paused",
		Help: "Whether an asynq queue is paused (1) or running (0).",
	}, []string{"queue"})

	// LogsDroppedTotal counts log records discarded because the OTLP shipping
	// queue was full.
	//
	// Logging must never block a request, so when the log backend cannot keep up
	// the record is dropped rather than waited on. Without this counter a drop is
	// silent, and "logs are missing" is indistinguishable from "nothing
	// happened" — the failure mode this whole observability epic exists to
	// remove.
	LogsDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "llm_logs_dropped_total",
		Help: "Total number of log records dropped because the OTLP queue was full.",
	})
)
