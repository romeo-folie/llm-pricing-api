package intelligence

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CapabilityScore mirrors a row in the model_capability_scores table.
type CapabilityScore struct {
	ID             uuid.UUID
	ModelID        int
	Dimension      string
	Score          *float64
	Confidence     string
	BenchmarkCount int
	Freshness      string
	ComputedAt     time.Time
}

// UpsertCapabilityScore inserts or updates a capability score row, matching on
// the (model_id, dimension) unique constraint.
func UpsertCapabilityScore(ctx context.Context, db *pgxpool.Pool, s CapabilityScore) error {
	_, err := db.Exec(ctx, `
		INSERT INTO model_capability_scores
		  (model_id, dimension, score, confidence, benchmark_count, freshness, computed_at)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
		ON CONFLICT (model_id, dimension)
		DO UPDATE SET
		  score           = EXCLUDED.score,
		  confidence      = EXCLUDED.confidence,
		  benchmark_count = EXCLUDED.benchmark_count,
		  freshness       = EXCLUDED.freshness,
		  computed_at     = NOW()
	`, s.ModelID, s.Dimension, s.Score, s.Confidence, s.BenchmarkCount, s.Freshness)
	return err
}

// GetCapabilityScores returns all capability scores for the given model.
func GetCapabilityScores(ctx context.Context, db *pgxpool.Pool, modelID int) ([]CapabilityScore, error) {
	rows, err := db.Query(ctx, `
		SELECT id, model_id, dimension, score, confidence, benchmark_count, freshness, computed_at
		FROM model_capability_scores
		WHERE model_id = $1
	`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CapabilityScore
	for rows.Next() {
		var s CapabilityScore
		if err := rows.Scan(
			&s.ID, &s.ModelID, &s.Dimension, &s.Score, &s.Confidence,
			&s.BenchmarkCount, &s.Freshness, &s.ComputedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
