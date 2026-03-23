package intelligence

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DimensionBenchmarks maps each capability dimension to its contributing
// benchmarks with relative weights (normalised at aggregation time).
// latency and cost_efficiency are derived from operational data, not benchmarks.
var DimensionBenchmarks = map[string]map[string]float64{
	"quality":           {"MMLU-Pro": 1.0, "GPQA Diamond": 1.0},
	"reasoning":         {"GPQA Diamond": 1.0, "MATH-500": 1.0, "AIME 2025": 1.0},
	"coding":            {"SWE-bench Verified": 1.0, "LiveCodeBench": 1.0},
	"tool_use":          {"BFCL V3": 1.0},
	"agentic":           {"SWE-bench Verified": 0.5, "BFCL V3": 0.5},
	"finance":           {"CFA": 1.0},
	"writing":           {"WritingBench": 1.0},
	"video":             {"Video-MME": 1.0},
	"instruction":       {"IFEval": 1.0},
	"preference":        {"Chatbot Arena": 1.0},
	"structured_output": {"BFCL V3": 0.5, "IFEval": 0.5},
}

// StalenessThresholdDays is the number of days after which a benchmark score
// is considered stale for freshness computation.
const StalenessThresholdDays = 90

// scoreEntry holds the resolved data needed for aggregation.
type scoreEntry struct {
	Normalized  float64
	EvaluatedAt time.Time
}

// AggregatedResult holds the computed capability score for one dimension.
type AggregatedResult struct {
	Dimension      string
	Score          float64
	Confidence     string
	BenchmarkCount int
	Freshness      string
}

// Aggregate computes capability scores for a single dimension given a map of
// benchmark_name → scoreEntry and the weight map for that dimension.
// It returns nil if no data is available for the dimension.
func Aggregate(dimension string, weights map[string]float64, scores map[string]scoreEntry, now time.Time) *AggregatedResult {
	var weightedSum, totalWeight float64
	var oldestEval time.Time
	count := 0

	for benchName, weight := range weights {
		entry, ok := scores[benchName]
		if !ok {
			continue
		}
		weightedSum += entry.Normalized * weight
		totalWeight += weight
		count++
		if oldestEval.IsZero() || entry.EvaluatedAt.Before(oldestEval) {
			oldestEval = entry.EvaluatedAt
		}
	}

	if count == 0 {
		return nil
	}

	aggScore := weightedSum / totalWeight

	freshness := "fresh"
	if !oldestEval.IsZero() && now.Sub(oldestEval) > time.Duration(StalenessThresholdDays)*24*time.Hour {
		freshness = "stale"
	}

	confidence := "low"
	if count >= 2 {
		confidence = "medium"
	}
	if float64(count) >= float64(len(weights)) {
		confidence = "high"
	}

	return &AggregatedResult{
		Dimension:      dimension,
		Score:          aggScore,
		Confidence:     confidence,
		BenchmarkCount: count,
		Freshness:      freshness,
	}
}

// benchmarkInfo holds the id-to-name mapping fetched from the benchmarks table.
type benchmarkInfo struct {
	id   string
	name string
}

// ComputeCapabilityScores reads all benchmark scores for a model, applies
// per-dimension weight formula, and upserts model_capability_scores.
// For batch recomputation across many models, prefer ComputeAllCapabilityScores
// which fetches the benchmarks table only once.
func ComputeCapabilityScores(ctx context.Context, db *pgxpool.Pool, modelID int) error {
	benchmarksByID, err := fetchBenchmarksByID(ctx, db)
	if err != nil {
		return err
	}
	return computeCapabilityScoresWithBenchmarks(ctx, db, modelID, benchmarksByID)
}

// fetchBenchmarksByID fetches the id→name mapping from the benchmarks table.
// The benchmarks table is static during a scoring run; callers should fetch
// it once and pass the result to computeCapabilityScoresWithBenchmarks.
func fetchBenchmarksByID(ctx context.Context, db *pgxpool.Pool) (map[string]benchmarkInfo, error) {
	rows, err := db.Query(ctx, `SELECT id::text, name FROM benchmarks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := map[string]benchmarkInfo{}
	for rows.Next() {
		var b benchmarkInfo
		if err := rows.Scan(&b.id, &b.name); err != nil {
			return nil, err
		}
		m[b.id] = b
	}
	return m, rows.Err()
}

// computeCapabilityScoresWithBenchmarks is the internal variant of
// ComputeCapabilityScores that accepts a pre-fetched benchmarksByID map,
// avoiding repeated DB round-trips in batch operations.
func computeCapabilityScoresWithBenchmarks(
	ctx context.Context,
	db *pgxpool.Pool,
	modelID int,
	benchmarksByID map[string]benchmarkInfo,
) error {
	scores, err := GetBenchmarkScores(ctx, db, modelID)
	if err != nil {
		return err
	}

	scoreByName := map[string]scoreEntry{}
	for _, s := range scores {
		if s.NormalizedScore == nil {
			continue
		}
		b, ok := benchmarksByID[s.BenchmarkID.String()]
		if !ok {
			continue
		}
		scoreByName[b.name] = scoreEntry{
			Normalized:  *s.NormalizedScore,
			EvaluatedAt: s.EvaluatedAt,
		}
	}

	now := time.Now()
	for dimension, weights := range DimensionBenchmarks {
		result := Aggregate(dimension, weights, scoreByName, now)
		if result == nil {
			continue
		}
		if err := UpsertCapabilityScore(ctx, db, CapabilityScore{
			ModelID:        modelID,
			Dimension:      result.Dimension,
			Score:          &result.Score,
			Confidence:     result.Confidence,
			BenchmarkCount: result.BenchmarkCount,
			Freshness:      result.Freshness,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ComputeAllCapabilityScores iterates all models with benchmark data and
// recomputes their capability scores. The benchmarks table is fetched once
// and reused across all models to avoid N+1 DB round-trips.
func ComputeAllCapabilityScores(ctx context.Context, db *pgxpool.Pool) error {
	// Hoist the static benchmarks lookup out of the per-model loop.
	benchmarksByID, err := fetchBenchmarksByID(ctx, db)
	if err != nil {
		return fmt.Errorf("fetch benchmarks: %w", err)
	}

	rows, err := db.Query(ctx, `SELECT DISTINCT model_id FROM model_benchmark_scores`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var modelIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		modelIDs = append(modelIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range modelIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := computeCapabilityScoresWithBenchmarks(ctx, db, id, benchmarksByID); err != nil {
			return fmt.Errorf("model %d: %w", id, err)
		}
	}
	return nil
}
