// Package intelligence provides storage and aggregation for model benchmark
// scores and computed capability scores.
package intelligence

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BenchmarkScore mirrors a row in the model_benchmark_scores table.
type BenchmarkScore struct {
	ID               uuid.UUID
	ModelID          int
	BenchmarkID      uuid.UUID
	RawScore         *float64
	NormalizedScore  *float64
	BenchmarkVersion string
	SourceURL        string
	Confidence       string
	EvaluatedAt      time.Time
	IngestedAt       time.Time
}

// UpsertBenchmarkScore inserts or updates a benchmark score row, matching on
// the (model_id, benchmark_id, benchmark_version) unique constraint.
func UpsertBenchmarkScore(ctx context.Context, db *pgxpool.Pool, s BenchmarkScore) error {
	_, err := db.Exec(ctx, `
		INSERT INTO model_benchmark_scores
		  (model_id, benchmark_id, raw_score, normalized_score,
		   benchmark_version, source_url, confidence, evaluated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (model_id, benchmark_id, benchmark_version)
		DO UPDATE SET
		  raw_score        = EXCLUDED.raw_score,
		  normalized_score = EXCLUDED.normalized_score,
		  source_url       = EXCLUDED.source_url,
		  confidence       = EXCLUDED.confidence,
		  evaluated_at     = EXCLUDED.evaluated_at,
		  ingested_at      = NOW()
	`, s.ModelID, s.BenchmarkID, s.RawScore, s.NormalizedScore,
		s.BenchmarkVersion, s.SourceURL, s.Confidence, s.EvaluatedAt)
	return err
}

// GetBenchmarkScores returns all benchmark scores for the given model.
func GetBenchmarkScores(ctx context.Context, db *pgxpool.Pool, modelID int) ([]BenchmarkScore, error) {
	rows, err := db.Query(ctx, `
		SELECT id, model_id, benchmark_id, raw_score, normalized_score,
		       benchmark_version, source_url, confidence, evaluated_at, ingested_at
		FROM model_benchmark_scores
		WHERE model_id = $1
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
			&s.BenchmarkVersion, &s.SourceURL, &s.Confidence, &s.EvaluatedAt, &s.IngestedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
