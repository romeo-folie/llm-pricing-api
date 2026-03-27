package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// feedbackAgg holds the aggregated signal for one model in a (use_case, priority) bucket.
type feedbackAgg struct {
	ModelSlug string
	AvgSignal float64
	Count     int
}

// CalibrateWeights runs the daily weight calibration loop.
//
// For each (use_case, priority) pair it:
//  1. Queries recommendation_feedback for the last 30 days.
//  2. Computes avg signal per model (only models with >= 10 votes).
//  3. Loads the current effective weights (from weight_preset_history, or hardcoded fallback).
//  4. Adjusts weights: negative signal reduces the primary dimension weight by 5%,
//     positive signal increases it by 5%, capped at ±20% of hardcoded baseline.
//  5. Writes updated weights to weight_preset_history.
func CalibrateWeights(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger) error {
	for uc := range ValidUseCases {
		for pri := range ValidPriorities {
			if err := calibratePair(ctx, db, log, uc, pri); err != nil {
				log.Error().Err(err).
					Str("use_case", string(uc)).
					Str("priority", string(pri)).
					Msg("calibration: pair failed")
				// Continue with remaining pairs.
			}
		}
	}
	return nil
}

func calibratePair(ctx context.Context, db *pgxpool.Pool, log zerolog.Logger, uc UseCase, pri Priority) error {
	// 1. Query feedback for last 30 days.
	aggs, err := queryFeedbackAgg(ctx, db, string(uc), 30, 10)
	if err != nil {
		return err
	}

	// 2. Load current effective weights.
	currentWeights, source, err := loadEffectiveWeights(ctx, db, string(uc), string(pri))
	if err != nil {
		return err
	}

	hardcoded := dimensionWeights(uc, pri)

	if len(aggs) == 0 {
		// No actionable feedback — ensure a row exists with the current weights.
		if source == "" {
			return writeWeights(ctx, db, string(uc), string(pri), hardcoded, "hardcoded")
		}
		return nil
	}

	// 3. Identify primary dimensions for this use case.
	primaryDims := PrimaryDimensions(uc)

	// 4. Adjust weights based on feedback signals.
	newWeights := make(map[string]float64, len(currentWeights))
	for dim, w := range currentWeights {
		newWeights[dim] = w
	}

	for _, agg := range aggs {
		if agg.AvgSignal < -0.3 {
			// Reduce primary dimension weights by 5%.
			for _, dim := range primaryDims {
				floor := hardcoded[dim] * 0.8
				adjusted := newWeights[dim] * 0.95
				newWeights[dim] = math.Max(floor, adjusted)
			}
		} else if agg.AvgSignal > 0.3 {
			// Increase primary dimension weights by 5%.
			for _, dim := range primaryDims {
				ceiling := hardcoded[dim] * 1.2
				adjusted := newWeights[dim] * 1.05
				newWeights[dim] = math.Min(ceiling, adjusted)
			}
		}
	}

	return writeWeights(ctx, db, string(uc), string(pri), newWeights, "calibrated")
}

// queryFeedbackAgg returns per-model average signals for the given use case
// within the last `days` days, only for models with at least `minVotes` votes.
func queryFeedbackAgg(ctx context.Context, db *pgxpool.Pool, useCase string, days, minVotes int) ([]feedbackAgg, error) {
	rows, err := db.Query(ctx, `
		SELECT model_slug, AVG(signal)::float8 AS avg_signal, COUNT(*)::int AS cnt
		FROM recommendation_feedback
		WHERE use_case = $1 AND created_at > NOW() - ($2 || ' days')::interval
		GROUP BY model_slug
		HAVING COUNT(*) >= $3
	`, useCase, days, minVotes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []feedbackAgg
	for rows.Next() {
		var a feedbackAgg
		if err := rows.Scan(&a.ModelSlug, &a.AvgSignal, &a.Count); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// loadEffectiveWeights returns the latest weights and source for a (use_case, priority) pair.
// If no row exists, returns the hardcoded weights with an empty source string.
func loadEffectiveWeights(ctx context.Context, db *pgxpool.Pool, useCase, priority string) (map[string]float64, string, error) {
	var weightsJSON []byte
	var source string
	err := db.QueryRow(ctx, `
		SELECT weights, source
		FROM weight_preset_history
		WHERE use_case = $1 AND priority = $2
		ORDER BY effective_at DESC
		LIMIT 1
	`, useCase, priority).Scan(&weightsJSON, &source)
	if err != nil {
		// Only fall back to hardcoded weights when no row exists.
		// Any other error (connection failure, query error, etc.) is a real
		// problem and must be surfaced to the caller.
		if errors.Is(err, pgx.ErrNoRows) {
			hardcoded := dimensionWeights(UseCase(useCase), Priority(priority))
			return hardcoded, "", nil
		}
		return nil, "", err
	}

	var weights map[string]float64
	if err := json.Unmarshal(weightsJSON, &weights); err != nil {
		return nil, "", err
	}
	return weights, source, nil
}

// writeWeights inserts a new row into weight_preset_history.
func writeWeights(ctx context.Context, db *pgxpool.Pool, useCase, priority string, weights map[string]float64, source string) error {
	weightsJSON, err := json.Marshal(weights)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO weight_preset_history (use_case, priority, weights, source)
		VALUES ($1, $2, $3, $4)
	`, useCase, priority, weightsJSON, source)
	return err
}

// GetEffectiveWeights returns the effective weights for a (use_case, priority)
// pair. It queries weight_preset_history for the latest row, falling back to
// the hardcoded dimensionWeights if no calibrated row exists.
func GetEffectiveWeights(ctx context.Context, db *pgxpool.Pool, useCase, priority string) (map[string]float64, error) {
	w, _, err := loadEffectiveWeights(ctx, db, useCase, priority)
	return w, err
}
