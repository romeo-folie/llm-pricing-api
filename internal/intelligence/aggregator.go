package intelligence

import (
	"context"
	"fmt"
	"sort"
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
	Confidence  string
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
	evidenceConfidence := "high"
	count := 0

	for benchName, weight := range weights {
		entry, ok := scores[benchName]
		if !ok {
			continue
		}
		weightedSum += entry.Normalized * weight
		totalWeight += weight
		count++
		evidenceConfidence = lowerConfidence(evidenceConfidence, entry.Confidence)
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
	// Coverage cannot make a capability more trustworthy than its underlying
	// evidence. This is especially important for SWE-bench, whose public scores
	// describe complete agent systems rather than isolated base-model ability.
	confidence = lowerConfidence(confidence, evidenceConfidence)

	return &AggregatedResult{
		Dimension:      dimension,
		Score:          aggScore,
		Confidence:     confidence,
		BenchmarkCount: count,
		Freshness:      freshness,
	}
}

func lowerConfidence(a, b string) string {
	normalize := func(value string) (string, int) {
		switch value {
		case "high":
			return "high", 2
		case "medium":
			return "medium", 1
		default:
			return "low", 0
		}
	}
	normalizedA, rankA := normalize(a)
	normalizedB, rankB := normalize(b)
	if rankB < rankA {
		return normalizedB
	}
	return normalizedA
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
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin capability replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Each live scraper triggers a full recomputation. Serialize replacement per
	// model so an overlapping transaction cannot commit results derived from an
	// older evidence snapshot after a newer replacement.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1280068944, $1)`, modelID); err != nil {
		return fmt.Errorf("lock model capability replacement: %w", err)
	}

	scores, err := GetActiveBenchmarkScores(ctx, tx, modelID)
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
			Confidence:  s.Confidence,
		}
	}

	now := time.Now()
	activeDimensions := make([]string, 0, len(DimensionBenchmarks))
	dimensions := make([]string, 0, len(DimensionBenchmarks))
	for dimension := range DimensionBenchmarks {
		dimensions = append(dimensions, dimension)
	}
	sort.Strings(dimensions)
	for _, dimension := range dimensions {
		weights := DimensionBenchmarks[dimension]
		result := Aggregate(dimension, weights, scoreByName, now)
		if result == nil {
			continue
		}
		if err := UpsertCapabilityScore(ctx, tx, CapabilityScore{
			ModelID:        modelID,
			Dimension:      result.Dimension,
			Score:          &result.Score,
			Confidence:     result.Confidence,
			BenchmarkCount: result.BenchmarkCount,
			Freshness:      result.Freshness,
		}); err != nil {
			return err
		}
		activeDimensions = append(activeDimensions, result.Dimension)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM model_capability_scores
		WHERE model_id = $1 AND NOT (dimension = ANY($2::text[]))
	`, modelID, activeDimensions); err != nil {
		return fmt.Errorf("delete obsolete capability scores: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit capability replacement: %w", err)
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

	// Include models that only have derived rows so deleted evidence removes
	// their obsolete capability scores on the next batch recomputation.
	rows, err := db.Query(ctx, `
		SELECT model_id FROM model_benchmark_scores
		UNION
		SELECT model_id FROM model_capability_scores
	`)
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
	rows.Close()

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
