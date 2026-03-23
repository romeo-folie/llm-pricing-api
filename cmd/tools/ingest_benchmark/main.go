// Command ingest_benchmark provides a CLI for manually inserting a single
// benchmark score and recomputing capability scores for the affected model.
//
// Usage:
//
//	go run cmd/tools/ingest_benchmark/main.go \
//	  --model=anthropic/claude-sonnet-4-6 \
//	  --benchmark="BFCL V4" \
//	  --score=79.4 \
//	  --source=https://gorilla.cs.berkeley.edu/leaderboard.html \
//	  --confidence=medium \
//	  --evaluated=2025-09-15
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"llm-pricing-api/internal/intelligence"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	model := flag.String("model", "", "Model slug (e.g. anthropic/claude-sonnet-4-6) — required")
	benchmark := flag.String("benchmark", "", "Benchmark name as stored in benchmarks.name — required")
	score := flag.Float64("score", 0, "Raw score (float) — required")
	source := flag.String("source", "", "Source URL — required")
	confidence := flag.String("confidence", "medium", "Confidence level: high, medium, low")
	evaluated := flag.String("evaluated", "", "Evaluation date YYYY-MM-DD (default: today)")
	flag.Parse()

	if *model == "" || *benchmark == "" || *source == "" {
		flag.Usage()
		return fmt.Errorf("--model, --benchmark, and --source are required")
	}
	if *score == 0 {
		// Allow explicit zero but flag was not set check.
		found := false
		flag.Visit(func(f *flag.Flag) {
			if f.Name == "score" {
				found = true
			}
		})
		if !found {
			return fmt.Errorf("--score is required")
		}
	}

	switch *confidence {
	case "high", "medium", "low":
	default:
		return fmt.Errorf("--confidence must be high, medium, or low; got %q", *confidence)
	}

	var evalTime time.Time
	if *evaluated != "" {
		t, err := time.Parse("2006-01-02", *evaluated)
		if err != nil {
			return fmt.Errorf("--evaluated must be YYYY-MM-DD: %w", err)
		}
		evalTime = t
	} else {
		evalTime = time.Now().UTC().Truncate(24 * time.Hour)
	}

	if *score > 100 || *score < 0 {
		fmt.Fprintf(os.Stderr, "warning: score %.2f is outside the expected 0–100 range\n", *score)
	}

	_ = godotenv.Load()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required")
	}

	ctx := context.Background()
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	// Validate model exists.
	modelID, err := intelligence.LookupModelIDBySlug(ctx, db, *model)
	if err != nil {
		return fmt.Errorf("model %q not found in database: %w", *model, err)
	}

	// Validate benchmark exists.
	benchmarkID, dbBenchmarkName, err := intelligence.LookupBenchmarkByName(ctx, db, *benchmark)
	if err != nil {
		return fmt.Errorf("benchmark %q not found in database: %w", *benchmark, err)
	}

	raw := *score
	norm := *score

	if err := intelligence.UpsertBenchmarkScore(ctx, db, intelligence.BenchmarkScore{
		ModelID:          modelID,
		BenchmarkID:      benchmarkID,
		RawScore:         &raw,
		NormalizedScore:  &norm,
		BenchmarkVersion: "manual-" + evalTime.Format("2006-01-02"),
		SourceURL:        *source,
		Confidence:       *confidence,
		EvaluatedAt:      evalTime,
	}); err != nil {
		return fmt.Errorf("upsert benchmark score: %w", err)
	}

	if err := intelligence.ComputeCapabilityScores(ctx, db, modelID); err != nil {
		return fmt.Errorf("recompute capability scores: %w", err)
	}

	fmt.Printf("✓ Inserted benchmark score for %q (benchmark: %s). Capability scores recomputed for model %s.\n",
		*model, dbBenchmarkName, *model)
	return nil
}
