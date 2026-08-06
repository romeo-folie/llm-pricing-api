//go:build integration

package intelligence

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func intelligenceTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database unavailable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func intelligenceFixtureModel(t *testing.T, db *pgxpool.Pool) int {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%s-%d", strings.ToLower(t.Name()), time.Now().UnixNano())
	var modelID int
	if err := db.QueryRow(ctx, `
		INSERT INTO models (name, slug, provider, modality)
		VALUES ($1, $2, 'integration-test', 'text') RETURNING id
	`, "intelligence "+suffix, "integration/intelligence-"+suffix).Scan(&modelID); err != nil {
		t.Fatalf("insert model fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM models WHERE id = $1`, modelID); err != nil {
			t.Logf("cleanup model %d: %v", modelID, err)
		}
	})
	return modelID
}

func benchmarkID(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id, err := LookupBenchmarkID(context.Background(), db, name)
	if err != nil {
		t.Fatalf("lookup benchmark %q: %v", name, err)
	}
	return id
}

func insertEvidence(t *testing.T, db *pgxpool.Pool, modelID int, benchmark uuid.UUID, version string, score *float64, evaluatedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO model_benchmark_scores
		  (model_id, benchmark_id, raw_score, normalized_score, benchmark_version,
		   source_url, confidence, evaluated_at)
		VALUES ($1, $2, $3, $3, $4, 'https://integration.example/evidence', 'high', $5)
	`, modelID, benchmark, score, version, evaluatedAt); err != nil {
		t.Fatalf("insert evidence %q: %v", version, err)
	}
}

func capabilityMap(t *testing.T, db *pgxpool.Pool, modelID int) map[string]CapabilityScore {
	t.Helper()
	scores, err := GetCapabilityScores(context.Background(), db, modelID)
	if err != nil {
		t.Fatalf("get capabilities: %v", err)
	}
	out := make(map[string]CapabilityScore, len(scores))
	for _, score := range scores {
		out[score.Dimension] = score
	}
	return out
}

func floatPtr(value float64) *float64 { return &value }

func TestIntegration_GetActiveBenchmarkScores_DeterministicOrdering(t *testing.T) {
	db := intelligenceTestPool(t)
	modelID := intelligenceFixtureModel(t, db)
	benchID := benchmarkID(t, db, "LiveCodeBench")
	ctx := context.Background()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	type row struct {
		id, version, modelName, entryName string
		score                             *float64
		evaluatedAt, ingestedAt           time.Time
		observedAt                        time.Time
	}
	observedAt := base.Add(10 * time.Hour)
	rows := []row{
		{"00000000-0000-0000-0000-000000000001", "z-version", "old-model", "old-entry", floatPtr(10), base, base.Add(5 * time.Hour), observedAt},
		{"00000000-0000-0000-0000-000000000002", "a-version", "new-model", "new-entry", floatPtr(20), base.Add(time.Hour), base, observedAt},
		{"00000000-0000-0000-0000-000000000003", "tie-ingest", "tie-model", "tie-entry", floatPtr(30), base.Add(time.Hour), base.Add(time.Hour), observedAt},
		{"00000000-0000-0000-0000-000000000004", "winner", "winner-model", "winner-entry", floatPtr(40), base.Add(time.Hour), base.Add(time.Hour), observedAt},
		{"00000000-0000-0000-0000-000000000005", "null-newest", "null-model", "null-entry", nil, base.Add(2 * time.Hour), base.Add(2 * time.Hour), observedAt.Add(time.Hour)},
	}
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if _, err := db.Exec(ctx, `
			INSERT INTO model_benchmark_scores
			  (id, model_id, benchmark_id, raw_score, normalized_score, benchmark_version,
			   source_url, source_model_name, source_entry_name, confidence, evaluated_at,
			   ingested_at, last_observed_at)
			VALUES ($1, $2, $3, $4, $4, $5, 'https://integration.example/active', $6, $7, 'high', $8, $9, $10)
		`, row.id, modelID, benchID, row.score, row.version, row.modelName, row.entryName, row.evaluatedAt, row.ingestedAt, row.observedAt); err != nil {
			t.Fatalf("insert active-order row: %v", err)
		}
	}

	active, err := GetActiveBenchmarkScores(ctx, db, modelID)
	if err != nil {
		t.Fatalf("GetActiveBenchmarkScores: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active rows = %d; want 1", len(active))
	}
	got := active[0]
	if got.ID.String() != rows[3].id || got.NormalizedScore == nil || *got.NormalizedScore != 40 {
		t.Fatalf("active row = %+v; want UUID %s score 40", got, rows[3].id)
	}
	if got.SourceModelName == nil || *got.SourceModelName != "winner-model" || got.SourceEntryName == nil || *got.SourceEntryName != "winner-entry" {
		t.Fatalf("winning provenance = (%v, %v); want winner identities", got.SourceModelName, got.SourceEntryName)
	}
}

func TestIntegration_GetActiveBenchmarkScores_InsertionOrderIndependent(t *testing.T) {
	db := intelligenceTestPool(t)
	firstModelID := intelligenceFixtureModel(t, db)
	secondModelID := intelligenceFixtureModel(t, db)
	benchID := benchmarkID(t, db, "LiveCodeBench")
	evaluatedAt := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	insert := func(modelID int, version string, score float64) {
		t.Helper()
		modelName, entryName := "same-model", "same-entry"
		if err := UpsertBenchmarkScore(context.Background(), db, BenchmarkScore{
			ModelID: modelID, BenchmarkID: benchID, RawScore: &score, NormalizedScore: &score,
			BenchmarkVersion: version, SourceURL: "https://integration.example/order",
			SourceModelName: &modelName, SourceEntryName: &entryName, Confidence: "high",
			EvaluatedAt: evaluatedAt, LastObservedAt: evaluatedAt.Add(time.Hour),
		}); err != nil {
			t.Fatalf("insert %s: %v", version, err)
		}
	}
	insert(firstModelID, "low", 20)
	insert(firstModelID, "high", 80)
	insert(secondModelID, "high", 80)
	insert(secondModelID, "low", 20)

	for _, modelID := range []int{firstModelID, secondModelID} {
		active, err := GetActiveBenchmarkScores(context.Background(), db, modelID)
		if err != nil {
			t.Fatal(err)
		}
		if len(active) != 1 || active[0].NormalizedScore == nil || *active[0].NormalizedScore != 80 {
			t.Fatalf("model %d active evidence = %+v; want score 80", modelID, active)
		}
	}
}

func TestIntegration_BenchmarkProvenance_LegacyNullsAndUpsert(t *testing.T) {
	db := intelligenceTestPool(t)
	modelID := intelligenceFixtureModel(t, db)
	benchID := benchmarkID(t, db, "LiveCodeBench")
	ctx := context.Background()
	score := 55.0

	insertEvidence(t, db, modelID, benchID, "legacy", &score, time.Now().Add(-time.Hour))
	active, err := GetActiveBenchmarkScores(ctx, db, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if active[0].SourceModelName != nil || active[0].SourceEntryName != nil {
		t.Fatalf("legacy provenance = (%v, %v); want nil", active[0].SourceModelName, active[0].SourceEntryName)
	}

	modelName, entryName := "upstream-model", "upstream-entry"
	firstEvaluation := time.Now().UTC().Truncate(time.Microsecond)
	firstObservation := firstEvaluation.Add(time.Hour)
	if err := UpsertBenchmarkScore(ctx, db, BenchmarkScore{
		ModelID: modelID, BenchmarkID: benchID, RawScore: &score, NormalizedScore: &score,
		BenchmarkVersion: "upsert", SourceURL: "https://integration.example/upsert",
		SourceModelName: &modelName, SourceEntryName: &entryName, Confidence: "medium",
		EvaluatedAt: firstEvaluation, LastObservedAt: firstObservation,
	}); err != nil {
		t.Fatalf("UpsertBenchmarkScore: %v", err)
	}
	active, err = GetActiveBenchmarkScores(ctx, db, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if active[0].SourceModelName == nil || *active[0].SourceModelName != modelName || active[0].SourceEntryName == nil || *active[0].SourceEntryName != entryName {
		t.Fatalf("upsert provenance not retained: %+v", active[0])
	}

	changedScore := 99.0
	changedModel, changedEntry := "mutated-model", "mutated-entry"
	if err := UpsertBenchmarkScore(ctx, db, BenchmarkScore{
		ModelID: modelID, BenchmarkID: benchID, RawScore: &changedScore, NormalizedScore: &changedScore,
		BenchmarkVersion: "upsert", SourceURL: "https://integration.example/mutated",
		SourceModelName: &changedModel, SourceEntryName: &changedEntry, Confidence: "high",
		EvaluatedAt: firstEvaluation.Add(24 * time.Hour), LastObservedAt: firstObservation.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("retry immutable evidence: %v", err)
	}
	active, err = GetActiveBenchmarkScores(ctx, db, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if active[0].NormalizedScore == nil || *active[0].NormalizedScore != score || !active[0].EvaluatedAt.Equal(firstEvaluation) {
		t.Fatalf("same-version retry mutated immutable evidence: %+v", active[0])
	}

	newScore := 56.0
	if err := UpsertBenchmarkScore(ctx, db, BenchmarkScore{
		ModelID: modelID, BenchmarkID: benchID, RawScore: &newScore, NormalizedScore: &newScore,
		BenchmarkVersion: "upsert-content-2", SourceURL: "https://integration.example/upsert",
		SourceModelName: &modelName, SourceEntryName: &entryName, Confidence: "medium",
		EvaluatedAt: firstEvaluation, LastObservedAt: firstObservation.Add(48 * time.Hour),
	}); err != nil {
		t.Fatalf("append changed evidence: %v", err)
	}
	var rowCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM model_benchmark_scores WHERE model_id = $1`, modelID).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 3 {
		t.Fatalf("history row count = %d; want legacy plus two immutable versions", rowCount)
	}
	active, err = GetActiveBenchmarkScores(ctx, db, modelID)
	if err != nil {
		t.Fatal(err)
	}
	if active[0].NormalizedScore == nil || *active[0].NormalizedScore != newScore {
		t.Fatalf("later downward correction did not become active: %+v", active[0])
	}
}

func TestIntegration_ComputeCapabilityScores_ReplacesExactDimensionSet(t *testing.T) {
	db := intelligenceTestPool(t)
	modelID := intelligenceFixtureModel(t, db)
	now := time.Now()
	insertEvidence(t, db, modelID, benchmarkID(t, db, "MMLU-Pro"), "quality", floatPtr(80), now)
	insertEvidence(t, db, modelID, benchmarkID(t, db, "Video-MME"), "video", floatPtr(70), now)
	if _, err := db.Exec(context.Background(), `
		INSERT INTO model_capability_scores (model_id, dimension, score, confidence, benchmark_count, freshness)
		VALUES ($1, 'finance', 99, 'high', 1, 'fresh')
	`, modelID); err != nil {
		t.Fatal(err)
	}

	if err := ComputeCapabilityScores(context.Background(), db, modelID); err != nil {
		t.Fatalf("initial recompute: %v", err)
	}
	capabilities := capabilityMap(t, db, modelID)
	if len(capabilities) != 2 || capabilities["quality"].Dimension == "" || capabilities["video"].Dimension == "" {
		t.Fatalf("dimensions = %v; want exactly quality and video", capabilities)
	}

	if _, err := db.Exec(context.Background(), `
		UPDATE model_benchmark_scores SET normalized_score = NULL
		WHERE model_id = $1 AND benchmark_id = $2
	`, modelID, benchmarkID(t, db, "Video-MME")); err != nil {
		t.Fatal(err)
	}
	if err := ComputeCapabilityScores(context.Background(), db, modelID); err != nil {
		t.Fatalf("replacement recompute: %v", err)
	}
	capabilities = capabilityMap(t, db, modelID)
	if len(capabilities) != 1 || capabilities["quality"].Dimension == "" {
		t.Fatalf("dimensions after evidence invalidation = %v; want only quality", capabilities)
	}
}

func TestIntegration_ComputeAllCapabilityScores_CleansModelWithZeroEvidence(t *testing.T) {
	db := intelligenceTestPool(t)
	modelID := intelligenceFixtureModel(t, db)
	insertEvidence(t, db, modelID, benchmarkID(t, db, "LiveCodeBench"), "coding", floatPtr(75), time.Now())
	if err := ComputeCapabilityScores(context.Background(), db, modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), `DELETE FROM model_benchmark_scores WHERE model_id = $1`, modelID); err != nil {
		t.Fatal(err)
	}
	if err := ComputeAllCapabilityScores(context.Background(), db); err != nil {
		t.Fatalf("ComputeAllCapabilityScores: %v", err)
	}
	if got := capabilityMap(t, db, modelID); len(got) != 0 {
		t.Fatalf("capabilities after all evidence removed = %v; want none", got)
	}
}

func TestIntegration_ComputeCapabilityScores_FreshnessIsPerDimension(t *testing.T) {
	db := intelligenceTestPool(t)
	modelID := intelligenceFixtureModel(t, db)
	now := time.Now()
	qualityID := benchmarkID(t, db, "MMLU-Pro")
	videoID := benchmarkID(t, db, "Video-MME")
	insertEvidence(t, db, modelID, qualityID, "quality-stale", floatPtr(80), now.Add(-120*24*time.Hour))
	insertEvidence(t, db, modelID, videoID, "video-old-history", floatPtr(10), now.Add(-150*24*time.Hour))
	insertEvidence(t, db, modelID, videoID, "video-active", floatPtr(90), now.Add(-24*time.Hour))

	if err := ComputeCapabilityScores(context.Background(), db, modelID); err != nil {
		t.Fatal(err)
	}
	capabilities := capabilityMap(t, db, modelID)
	if capabilities["quality"].Freshness != "stale" {
		t.Fatalf("quality freshness = %q; want stale", capabilities["quality"].Freshness)
	}
	if capabilities["video"].Freshness != "fresh" {
		t.Fatalf("video freshness = %q; want fresh", capabilities["video"].Freshness)
	}
}

func TestIntegration_ComputeCapabilityScores_RollsBackWholeReplacement(t *testing.T) {
	db := intelligenceTestPool(t)
	modelID := intelligenceFixtureModel(t, db)
	ctx := context.Background()
	insertEvidence(t, db, modelID, benchmarkID(t, db, "MMLU-Pro"), "quality", floatPtr(80), time.Now())
	oldComputedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.Exec(ctx, `
		INSERT INTO model_capability_scores
		  (model_id, dimension, score, confidence, benchmark_count, freshness, computed_at)
		VALUES ($1, 'quality', 10, 'low', 1, 'stale', $2),
		       ($1, 'finance', 20, 'low', 1, 'stale', $2)
	`, modelID, oldComputedAt); err != nil {
		t.Fatal(err)
	}

	functionName := fmt.Sprintf("integration_fail_capability_delete_%d", modelID)
	triggerName := fmt.Sprintf("integration_fail_capability_delete_trigger_%d", modelID)
	cleanupDDL := func() {
		_, _ = db.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON model_capability_scores`)
		_, _ = db.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+functionName+`()`) // test-owned function
	}
	cleanupDDL()
	t.Cleanup(cleanupDDL)
	ddl := fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		  IF OLD.model_id = %d THEN RAISE EXCEPTION 'forced integration delete failure'; END IF;
		  RETURN OLD;
		END $$;
		CREATE TRIGGER %s BEFORE DELETE ON model_capability_scores
		FOR EACH ROW EXECUTE FUNCTION %s();
	`, functionName, modelID, triggerName, functionName)
	if _, err := db.Exec(ctx, ddl); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	if err := ComputeCapabilityScores(ctx, db, modelID); err == nil {
		t.Fatal("recompute succeeded; want forced delete failure")
	}
	capabilities := capabilityMap(t, db, modelID)
	if len(capabilities) != 2 || capabilities["quality"].Score == nil || *capabilities["quality"].Score != 10 || !capabilities["quality"].ComputedAt.Equal(oldComputedAt) {
		t.Fatalf("replacement was not rolled back: %v", capabilities)
	}

	cleanupDDL()
	if err := ComputeCapabilityScores(ctx, db, modelID); err != nil {
		t.Fatalf("recompute after removing trigger: %v", err)
	}
	capabilities = capabilityMap(t, db, modelID)
	if len(capabilities) != 1 || capabilities["quality"].Score == nil || *capabilities["quality"].Score != 80 {
		t.Fatalf("replacement after retry = %v; want quality score 80", capabilities)
	}
}
