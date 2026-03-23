package intelligence

import (
	"testing"
	"time"
)

func TestAggregate_WeightedAverage(t *testing.T) {
	now := time.Now()
	recent := now.Add(-24 * time.Hour)

	scores := map[string]scoreEntry{
		"MMLU-Pro":     {Normalized: 80, EvaluatedAt: recent},
		"GPQA Diamond": {Normalized: 60, EvaluatedAt: recent},
	}

	result := Aggregate("quality", DimensionBenchmarks["quality"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result for quality dimension")
	}

	// Both benchmarks have weight 1.0, so average = (80+60)/2 = 70.
	if result.Score != 70.0 {
		t.Errorf("expected score 70.0, got %f", result.Score)
	}
	if result.Dimension != "quality" {
		t.Errorf("expected dimension 'quality', got %q", result.Dimension)
	}
}

func TestAggregate_SingleBenchmark_LowConfidence(t *testing.T) {
	now := time.Now()
	recent := now.Add(-24 * time.Hour)

	// tool_use only has one benchmark (BFCL V4), so even with it present
	// confidence should be "high" since count == len(weights).
	scores := map[string]scoreEntry{
		"BFCL V4": {Normalized: 90, EvaluatedAt: recent},
	}

	result := Aggregate("tool_use", DimensionBenchmarks["tool_use"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Confidence != "high" {
		t.Errorf("expected confidence 'high' when all benchmarks present, got %q", result.Confidence)
	}

	// For reasoning (3 benchmarks), providing only 1 → confidence = "low".
	result = Aggregate("reasoning", DimensionBenchmarks["reasoning"], scores, now)
	if result != nil {
		t.Error("expected nil result for reasoning with only BFCL V4 data")
	}

	// Provide one reasoning benchmark.
	reasoningScores := map[string]scoreEntry{
		"MATH-500": {Normalized: 75, EvaluatedAt: recent},
	}
	result = Aggregate("reasoning", DimensionBenchmarks["reasoning"], reasoningScores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Confidence != "low" {
		t.Errorf("expected confidence 'low' for 1 of 3 benchmarks, got %q", result.Confidence)
	}
	if result.BenchmarkCount != 1 {
		t.Errorf("expected benchmark_count 1, got %d", result.BenchmarkCount)
	}
}

func TestAggregate_AllBenchmarks_HighConfidence(t *testing.T) {
	now := time.Now()
	recent := now.Add(-24 * time.Hour)

	// Provide all 3 reasoning benchmarks.
	scores := map[string]scoreEntry{
		"GPQA Diamond": {Normalized: 60, EvaluatedAt: recent},
		"MATH-500":     {Normalized: 80, EvaluatedAt: recent},
		"AIME 2025":    {Normalized: 40, EvaluatedAt: recent},
	}

	result := Aggregate("reasoning", DimensionBenchmarks["reasoning"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Confidence != "high" {
		t.Errorf("expected confidence 'high', got %q", result.Confidence)
	}
	if result.BenchmarkCount != 3 {
		t.Errorf("expected benchmark_count 3, got %d", result.BenchmarkCount)
	}
	// All weights 1.0 → average = (60+80+40)/3 = 60.
	expected := 60.0
	if result.Score != expected {
		t.Errorf("expected score %f, got %f", expected, result.Score)
	}
}

func TestAggregate_MediumConfidence(t *testing.T) {
	now := time.Now()
	recent := now.Add(-24 * time.Hour)

	// 2 of 3 reasoning benchmarks → "medium".
	scores := map[string]scoreEntry{
		"GPQA Diamond": {Normalized: 70, EvaluatedAt: recent},
		"MATH-500":     {Normalized: 90, EvaluatedAt: recent},
	}

	result := Aggregate("reasoning", DimensionBenchmarks["reasoning"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Confidence != "medium" {
		t.Errorf("expected confidence 'medium', got %q", result.Confidence)
	}
}

func TestAggregate_Staleness(t *testing.T) {
	now := time.Now()

	// Score older than 90 days → stale.
	staleDate := now.Add(-100 * 24 * time.Hour)
	scores := map[string]scoreEntry{
		"MMLU-Pro":     {Normalized: 80, EvaluatedAt: staleDate},
		"GPQA Diamond": {Normalized: 60, EvaluatedAt: now.Add(-24 * time.Hour)},
	}

	result := Aggregate("quality", DimensionBenchmarks["quality"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Freshness != "stale" {
		t.Errorf("expected freshness 'stale' when oldest score > 90 days, got %q", result.Freshness)
	}

	// Both recent → fresh.
	freshScores := map[string]scoreEntry{
		"MMLU-Pro":     {Normalized: 80, EvaluatedAt: now.Add(-24 * time.Hour)},
		"GPQA Diamond": {Normalized: 60, EvaluatedAt: now.Add(-48 * time.Hour)},
	}
	result = Aggregate("quality", DimensionBenchmarks["quality"], freshScores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Freshness != "fresh" {
		t.Errorf("expected freshness 'fresh', got %q", result.Freshness)
	}
}

func TestAggregate_NoData_ReturnsNil(t *testing.T) {
	now := time.Now()
	empty := map[string]scoreEntry{}

	result := Aggregate("quality", DimensionBenchmarks["quality"], empty, now)
	if result != nil {
		t.Error("expected nil result when no score data available")
	}
}

func TestAggregate_UnequalWeights(t *testing.T) {
	now := time.Now()
	recent := now.Add(-24 * time.Hour)

	// structured_output: BFCL V4 (0.5) + IFEval (0.5)
	scores := map[string]scoreEntry{
		"BFCL V4": {Normalized: 80, EvaluatedAt: recent},
		"IFEval":  {Normalized: 60, EvaluatedAt: recent},
	}

	result := Aggregate("structured_output", DimensionBenchmarks["structured_output"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// (80*0.5 + 60*0.5) / (0.5+0.5) = 70
	if result.Score != 70.0 {
		t.Errorf("expected score 70.0, got %f", result.Score)
	}
}

func TestAggregate_AsymmetricWeights(t *testing.T) {
	now := time.Now()
	recent := now.Add(-24 * time.Hour)

	// agentic: SWE-bench Verified (0.5) + BFCL V4 (0.5)
	// Only one present → score = that benchmark's value.
	scores := map[string]scoreEntry{
		"BFCL V4": {Normalized: 90, EvaluatedAt: recent},
	}

	result := Aggregate("agentic", DimensionBenchmarks["agentic"], scores, now)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// (90*0.5) / 0.5 = 90
	if result.Score != 90.0 {
		t.Errorf("expected score 90.0, got %f", result.Score)
	}
	// 1 of 2 benchmarks → low confidence.
	if result.Confidence != "low" {
		t.Errorf("expected confidence 'low', got %q", result.Confidence)
	}
}
