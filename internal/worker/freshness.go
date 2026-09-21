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

// sourceFreshness is one aggregated row of per-source price freshness.
type sourceFreshness struct {
	// Source is the sources.name value (e.g. "openrouter", "litellm").
	Source string
	// LastVerifiedAt is the most recent verification stamp across the source's
	// published prices, falling back to confirmed_at for rows written before
	// last_verified_at existed — the same fallback the API read path applies.
	LastVerifiedAt time.Time
	// Prices is the number of published price rows for the source.
	Prices int64
	// Stale is the subset of Prices older than the sampler's threshold.
	Stale int64
}

// freshnessQuerier is the narrow read interface Sample needs.
//
// It is deliberately separate from WorkerStore: the sampler is the only caller,
// and a new WorkerStore method would force every existing mock to grow a stub
// for a query the scrape pipeline never runs.
type freshnessQuerier interface {
	SourceFreshness(ctx context.Context, staleAfter time.Duration) ([]sourceFreshness, error)
}

// pgxFreshnessStore is the PostgreSQL-backed freshnessQuerier.
type pgxFreshnessStore struct {
	db *pgxpool.Pool
}

// SourceFreshness runs a single grouped query over prices joined to sources and
// returns one row per source that has published prices. staleAfter is bound as a
// query parameter (never interpolated) and compared against the same COALESCE
// expression the API uses for freshness, so legacy rows with a NULL
// last_verified_at are measured from confirmed_at instead of being invisible.
func (s *pgxFreshnessStore) SourceFreshness(ctx context.Context, staleAfter time.Duration) ([]sourceFreshness, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.name,
		       MAX(COALESCE(p.last_verified_at, p.confirmed_at)) AS last_verified_at,
		       COUNT(*)                                          AS price_count,
		       COUNT(*) FILTER (
		           WHERE COALESCE(p.last_verified_at, p.confirmed_at) < NOW() - $1::interval
		       )                                                 AS stale_count
		FROM prices p
		JOIN sources s ON s.id = p.source_id
		GROUP BY s.name
	`, staleAfter)
	if err != nil {
		return nil, fmt.Errorf("freshness store: query source freshness: %w", err)
	}
	defer rows.Close()

	var result []sourceFreshness
	for rows.Next() {
		var f sourceFreshness
		if err := rows.Scan(&f.Source, &f.LastVerifiedAt, &f.Prices, &f.Stale); err != nil {
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
	querier    freshnessQuerier
	staleAfter time.Duration
}

// NewFreshnessSampler returns a sampler backed by the provided connection pool.
// staleAfter is the age beyond which a price counts as stale; production passes
// DefaultStaleAfter.
func NewFreshnessSampler(db *pgxpool.Pool, staleAfter time.Duration) *FreshnessSampler {
	return &FreshnessSampler{querier: &pgxFreshnessStore{db: db}, staleAfter: staleAfter}
}

// Sample runs one freshness query and republishes the gauges.
//
// Sources the query does not return — those with no published prices — are left
// untouched rather than zeroed: a zeroed ratio reads as "perfectly fresh" and a
// zeroed last-success timestamp reads as "verified in 1970", both of which would
// mislead the dashboard and the alert rules. On a query error nothing is
// published and the error is returned so the caller can log it.
func (s *FreshnessSampler) Sample(ctx context.Context) error {
	rows, err := s.querier.SourceFreshness(ctx, s.staleAfter)
	if err != nil {
		return fmt.Errorf("freshness sampler: %w", err)
	}

	for _, row := range rows {
		if row.Prices <= 0 {
			// The grouped query cannot return this, but setting it would divide
			// by zero and publish a "healthy" ratio for a source with no data.
			continue
		}
		metrics.SourceLastSuccessTimestampSeconds.WithLabelValues(row.Source).
			Set(float64(row.LastVerifiedAt.Unix()))
		metrics.PricesPublished.WithLabelValues(row.Source).Set(float64(row.Prices))
		metrics.PricesStaleRatio.WithLabelValues(row.Source).
			Set(float64(row.Stale) / float64(row.Prices))
	}
	return nil
}
