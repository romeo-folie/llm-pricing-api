package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/metrics"
)

// DefaultStaleAfter is the age beyond which a published price counts as stale.
// It encodes the product promise ("no published value older than 24 hours
// without a stale indicator") and matches the 24h medium-confidence window in
// internal/api.ComputeTrustMeta. The two thresholds are the same promise seen
// from two sides; they must move together.
const DefaultStaleAfter = 24 * time.Hour

// DefaultActiveWindow is how recently a price must have been verified for its
// model to count as active. Sources are scraped at least daily, so a model not
// re-verified in a week has been delisted or renamed upstream — or orphaned by a
// scraper change — and must not keep inflating the stale ratio.
//
// Without this window the ratio measures accumulated history: it climbs
// monotonically as upstream churns and eventually pins near 1 for every source,
// which is why LLMPricesStaleRatioHigh was firing on feeds that were working
// correctly (#217).
//
// It does not hide a source that stops scraping entirely: its models stay active
// (and therefore stale) for a week while the ratio climbs, and
// LLMSourceFreshnessStale — which reads the last-success timestamp rather than
// the ratio — alerts immediately.
const DefaultActiveWindow = 7 * 24 * time.Hour

// sourceFreshness is one aggregated row of per-source price freshness.
type sourceFreshness struct {
	// Source is the sources.name value (e.g. "openrouter", "litellm").
	Source string
	// LastVerifiedAt is the most recent verification stamp across the source's
	// published prices, falling back to confirmed_at for rows written before
	// last_verified_at existed — the same fallback the API read path applies.
	LastVerifiedAt time.Time
	// Prices is every published price row for the source, at any age. It gives
	// the gauges absolute context.
	Prices int64
	// Active is the subset of Prices verified inside the sampler's activity
	// window — the models the pipeline still expects to see. Rows outside it are
	// delisted or orphaned and are excluded from the stale ratio.
	Active int64
	// Stale is the subset of Active older than the sampler's staleness threshold.
	Stale int64
}

// freshnessQuerier is the narrow read interface Sample needs.
//
// It is deliberately separate from WorkerStore: the sampler is the only caller,
// and a new WorkerStore method would force every existing mock to grow a stub
// for a query the scrape pipeline never runs.
type freshnessQuerier interface {
	SourceFreshness(ctx context.Context, staleAfter, activeWindow time.Duration) ([]sourceFreshness, error)
}

// pgxFreshnessStore is the PostgreSQL-backed freshnessQuerier.
type pgxFreshnessStore struct {
	db *pgxpool.Pool
}

// SourceFreshness runs a single grouped query over prices joined to sources and
// returns one row per source that has published prices. staleAfter and
// activeWindow are bound as query parameters (never interpolated) and compared
// against the same COALESCE expression the API uses for freshness, so legacy
// rows with a NULL last_verified_at are measured from confirmed_at instead of
// being invisible.
func (s *pgxFreshnessStore) SourceFreshness(ctx context.Context, staleAfter, activeWindow time.Duration) ([]sourceFreshness, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.name,
		       MAX(COALESCE(p.last_verified_at, p.confirmed_at)) AS last_verified_at,
		       COUNT(*)                                          AS price_count,
		       COUNT(*) FILTER (
		           WHERE COALESCE(p.last_verified_at, p.confirmed_at) >= NOW() - $2::interval
		       )                                                 AS active_count,
		       COUNT(*) FILTER (
		           WHERE COALESCE(p.last_verified_at, p.confirmed_at) >= NOW() - $2::interval
		             AND COALESCE(p.last_verified_at, p.confirmed_at) <  NOW() - $1::interval
		       )                                                 AS stale_count
		FROM prices p
		JOIN sources s ON s.id = p.source_id
		GROUP BY s.name
	`, staleAfter, activeWindow)
	if err != nil {
		return nil, fmt.Errorf("freshness store: query source freshness: %w", err)
	}
	defer rows.Close()

	var result []sourceFreshness
	for rows.Next() {
		var f sourceFreshness
		if err := rows.Scan(&f.Source, &f.LastVerifiedAt, &f.Prices, &f.Active, &f.Stale); err != nil {
			return nil, fmt.Errorf("freshness store: scan freshness row: %w", err)
		}
		result = append(result, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("freshness store: iterate freshness rows: %w", err)
	}
	return result, nil
}

// FreshnessSampler republishes per-source price freshness as Prometheus gauges.
//
// It is meant to run on a periodic ticker (see cmd/worker), independently of the
// scrape pipeline. That independence is the whole point: a sampler invoked only
// inside the pipeline would leave the gauges frozen at their last good values if
// a scraper silently stopped running, so `time() - last success` would stay
// small and the staleness alert would stay silent.
type FreshnessSampler struct {
	// querier is an interface rather than the concrete *pgxpool.Pool so the
	// sampling logic is unit-testable with a mock; NewFreshnessSampler wires the
	// pgx implementation for production.
	querier      freshnessQuerier
	staleAfter   time.Duration
	activeWindow time.Duration
}

// NewFreshnessSampler returns a sampler backed by the provided connection pool.
// staleAfter is the age beyond which a price counts as stale; production passes
// DefaultStaleAfter. The activity window is DefaultActiveWindow.
func NewFreshnessSampler(db *pgxpool.Pool, staleAfter time.Duration) *FreshnessSampler {
	return &FreshnessSampler{
		querier:      &pgxFreshnessStore{db: db},
		staleAfter:   staleAfter,
		activeWindow: DefaultActiveWindow,
	}
}

// Sample runs one freshness query and republishes the gauges.
//
// Sources the query does not return — those with no published prices — are left
// untouched rather than zeroed: a zeroed ratio reads as "perfectly fresh" and a
// zeroed last-success timestamp reads as "verified in 1970", both of which would
// mislead the dashboard and the alert rules. On a query error nothing is
// published and the error is returned so the caller can log it.
func (s *FreshnessSampler) Sample(ctx context.Context) error {
	rows, err := s.querier.SourceFreshness(ctx, s.staleAfter, s.activeWindow)
	if err != nil {
		return fmt.Errorf("freshness sampler: %w", err)
	}

	for _, row := range rows {
		if row.Prices <= 0 {
			// The grouped query cannot return this, but setting it would publish
			// "verified in 1970" and a "healthy" ratio for a source with no data.
			continue
		}
		metrics.SourceLastSuccessTimestampSeconds.WithLabelValues(row.Source).
			Set(float64(row.LastVerifiedAt.Unix()))
		metrics.PricesPublished.WithLabelValues(row.Source).Set(float64(row.Prices))

		if row.Active <= 0 {
			// Every model for this source has aged out of the activity window:
			// the whole feed is delisted, or it stopped verifying so long ago
			// that nothing counts as active. There is no denominator for a
			// ratio; LLMSourceFreshnessStale covers the stopped case.
			continue
		}
		metrics.PricesActive.WithLabelValues(row.Source).Set(float64(row.Active))
		metrics.PricesStaleRatio.WithLabelValues(row.Source).
			Set(float64(row.Stale) / float64(row.Active))
	}
	return nil
}
