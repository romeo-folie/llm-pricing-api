// Package intelligence provides storage and aggregation for model benchmark
// scores and computed capability scores.
package intelligence

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the subset of pgx pool and transaction behavior needed by the
// intelligence stores. Accepting this interface lets recomputation replace a
// model's derived capability rows atomically.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// BenchmarkScore mirrors a row in the model_benchmark_scores table.
type BenchmarkScore struct {
	ID               uuid.UUID
	ModelID          int
	BenchmarkID      uuid.UUID
	RawScore         *float64
	NormalizedScore  *float64
	BenchmarkVersion string
	SourceURL        string
	SourceModelName  *string
	SourceEntryName  *string
	Confidence       string
	EvaluatedAt      time.Time
	IngestedAt       time.Time
	LastObservedAt   time.Time
}

// UpsertBenchmarkScore idempotently inserts immutable benchmark evidence.
// Producers use a new version whenever score or provenance content changes;
// retrying the same evidence is a no-op.
func UpsertBenchmarkScore(ctx context.Context, db DBTX, s BenchmarkScore) error {
	_, err := db.Exec(ctx, `
		INSERT INTO model_benchmark_scores
		  (model_id, benchmark_id, raw_score, normalized_score,
		   benchmark_version, source_url, source_model_name, source_entry_name,
		   confidence, evaluated_at, last_observed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (model_id, benchmark_id, benchmark_version)
		DO UPDATE SET last_observed_at = GREATEST(
		  model_benchmark_scores.last_observed_at,
		  EXCLUDED.last_observed_at
		)
	`, s.ModelID, s.BenchmarkID, s.RawScore, s.NormalizedScore,
		s.BenchmarkVersion, s.SourceURL, s.SourceModelName, s.SourceEntryName,
		s.Confidence, s.EvaluatedAt, s.LastObservedAt)
	return err
}

// GetActiveBenchmarkScores returns exactly one active score per benchmark for
// the given model. Source snapshot observation time selects the currently
// observed evidence; evaluation time and stable content resolve ties. Version
// strings are never parsed to infer recency.
func GetActiveBenchmarkScores(ctx context.Context, db DBTX, modelID int) ([]BenchmarkScore, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (benchmark_id)
		       id, model_id, benchmark_id, raw_score, normalized_score,
		       benchmark_version, source_url, source_model_name, source_entry_name,
		       confidence, evaluated_at, ingested_at, last_observed_at
		FROM model_benchmark_scores
		WHERE model_id = $1 AND normalized_score IS NOT NULL
		ORDER BY benchmark_id,
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
	`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BenchmarkScore
	for rows.Next() {
		var s BenchmarkScore
		if err := rows.Scan(
			&s.ID, &s.ModelID, &s.BenchmarkID, &s.RawScore, &s.NormalizedScore,
			&s.BenchmarkVersion, &s.SourceURL, &s.SourceModelName, &s.SourceEntryName,
			&s.Confidence, &s.EvaluatedAt, &s.IngestedAt, &s.LastObservedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
