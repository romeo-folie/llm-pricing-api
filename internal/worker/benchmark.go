package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/intelligence"
	"llm-pricing-api/internal/metrics"
)

// DefaultBenchmarkStaleAfter is the age beyond which active benchmark evidence
// counts as stale.
//
// It is derived from intelligence.StalenessThresholdDays rather than declared
// independently: the capability scorer marks a dimension "stale" on exactly
// that threshold, so a hard-coded 90 here could silently drift from the rule
// the product actually applies to evidence.
const DefaultBenchmarkStaleAfter = time.Duration(intelligence.StalenessThresholdDays) * 24 * time.Hour

// benchmarkEvidence is one aggregated row of per-benchmark evidence freshness.
type benchmarkEvidence struct {
	// Benchmark is the benchmarks.name value (e.g. "SWE-bench Verified").
	Benchmark string
	// Active is the number of active evidence rows: one normalised score per
	// (model, benchmark), the same row the scorer uses.
	Active int64
	// Stale is the subset of Active whose evaluated_at is older than the
	// sampler's threshold.
	Stale int64
}

// benchmarkEvidenceQuerier is the narrow read interface Sample needs.
//
// Like freshnessQuerier it is deliberately separate from WorkerStore: the
// sampler is the only caller, and adding the method to WorkerStore would force
// every existing mock to grow a stub for a query the scrape pipeline never runs.
type benchmarkEvidenceQuerier interface {
	BenchmarkEvidenceFreshness(ctx context.Context, staleAfter time.Duration) ([]benchmarkEvidence, error)
}

// pgxBenchmarkEvidenceStore is the PostgreSQL-backed benchmarkEvidenceQuerier.
type pgxBenchmarkEvidenceStore struct {
	db *pgxpool.Pool
}

// BenchmarkEvidenceFreshness runs a single grouped query over active benchmark
// evidence and returns one row per benchmark that has any.
//
// "Active" mirrors intelligence.GetActiveBenchmarkScores: exactly one
// normalised row per (model, benchmark), selected by the same ordering —
// source observation time first, then evaluation time, then the stable
// content tie-breakers. If that ordering changes in the scorer, this query must
// change with it, or the gauge will describe a different row than the one that
// produced the capability score.
//
// staleAfter is bound as a query parameter (never interpolated) and compared
// against evaluated_at, not last_observed_at. SWE-bench is re-scraped daily, so
// last_observed_at is always fresh while the newest published evaluation can be
// months old; measuring observation time would report the benchmark as healthy
// while the scorer treats its evidence as stale.
func (s *pgxBenchmarkEvidenceStore) BenchmarkEvidenceFreshness(ctx context.Context, staleAfter time.Duration) ([]benchmarkEvidence, error) {
	rows, err := s.db.Query(ctx, `
		SELECT b.name,
		       COUNT(*) AS active_count,
		       COUNT(*) FILTER (WHERE e.evaluated_at < NOW() - $1::interval) AS stale_count
		FROM (
			SELECT DISTINCT ON (model_id, benchmark_id)
			       model_id, benchmark_id, evaluated_at
			FROM model_benchmark_scores
			WHERE normalized_score IS NOT NULL
			ORDER BY model_id,
			         benchmark_id,
			         last_observed_at DESC,
			         evaluated_at DESC,
			         normalized_score DESC,
			         raw_score DESC NULLS LAST,
			         source_model_name ASC NULLS LAST,
			         source_entry_name ASC NULLS LAST,
			         source_url ASC,
			         confidence ASC,
			         benchmark_version ASC,
			         id DESC
		) e
		JOIN benchmarks b ON b.id = e.benchmark_id
		GROUP BY b.name
		ORDER BY b.name
	`, staleAfter)
	if err != nil {
		return nil, fmt.Errorf("benchmark evidence store: query freshness: %w", err)
	}
	defer rows.Close()

	var result []benchmarkEvidence
	for rows.Next() {
		var e benchmarkEvidence
		if err := rows.Scan(&e.Benchmark, &e.Active, &e.Stale); err != nil {
			return nil, fmt.Errorf("benchmark evidence store: scan freshness row: %w", err)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("benchmark evidence store: iterate freshness rows: %w", err)
	}
	return result, nil
}

// BenchmarkSampler republishes benchmark-evidence coverage and freshness as
// Prometheus gauges.
//
// It is meant to run on the same kind of periodic ticker as FreshnessSampler
// (see cmd/worker), independently of the benchmark scrape pipeline. A sampler
// invoked only inside the pipeline would leave the gauges frozen at their last
// good values between daily scrapes, so a benchmark whose evidence ages past
// the threshold would look fresh until the next run.
type BenchmarkSampler struct {
	// querier is an interface rather than the concrete *pgxpool.Pool so the
	// sampling logic is unit-testable with a mock; NewBenchmarkSampler wires the
	// pgx implementation for production.
	querier    benchmarkEvidenceQuerier
	staleAfter time.Duration
}

// NewBenchmarkSampler returns a sampler backed by the provided connection pool.
// staleAfter is the age beyond which evidence counts as stale; production
// passes DefaultBenchmarkStaleAfter.
func NewBenchmarkSampler(db *pgxpool.Pool, staleAfter time.Duration) *BenchmarkSampler {
	return &BenchmarkSampler{querier: &pgxBenchmarkEvidenceStore{db: db}, staleAfter: staleAfter}
}

// Sample runs one evidence-freshness query and republishes the gauges.
//
// Benchmarks the query does not return — those with no active evidence — are
// left untouched rather than zeroed: a zeroed stale ratio reads as "perfectly
// fresh" and a zeroed evidence count is indistinguishable from a benchmark that
// has simply never been ingested, which is exactly the state the audit needs to
// see. On a query error nothing is published and the error is returned so the
// caller can log it.
func (s *BenchmarkSampler) Sample(ctx context.Context) error {
	rows, err := s.querier.BenchmarkEvidenceFreshness(ctx, s.staleAfter)
	if err != nil {
		return fmt.Errorf("benchmark sampler: %w", err)
	}

	for _, row := range rows {
		if row.Active <= 0 {
			// The grouped query cannot return this, but setting it would divide
			// by zero and publish a "healthy" ratio for a benchmark with no data.
			continue
		}
		metrics.BenchmarkEvidenceActive.WithLabelValues(row.Benchmark).Set(float64(row.Active))
		metrics.BenchmarkEvidenceStaleRatio.WithLabelValues(row.Benchmark).
			Set(float64(row.Stale) / float64(row.Active))
	}
	return nil
}
