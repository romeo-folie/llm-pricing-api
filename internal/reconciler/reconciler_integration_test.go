//go:build integration

package reconciler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/reconciler"
)

// testDB creates a connection pool from DATABASE_URL. Skips the test if the
// env var is absent or the database is unreachable (keeps CI clean without Docker).
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// DATABASE_URL is read directly here (bypassing config.Load) because this
	// helper must skip—not fail—when the variable is absent, preserving
	// CI-safe behaviour. Production code must always use config.Load() instead.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("cannot connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// fixture holds the IDs and names inserted by insertFixture.
type fixture struct {
	modelID int
	srcAID  int
	srcBID  int
	slug    string
	srcA    string
	srcB    string
}

// insertFixture inserts a model and two sources with unique names (derived from
// the test name + timestamp to avoid collisions) and registers cleanup.
func insertFixture(t *testing.T, db *pgxpool.Pool) fixture {
	t.Helper()
	ctx := context.Background()
	// Use a unique suffix so parallel test runs don't collide on UNIQUE constraints.
	suffix := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	slug := "test-model/" + suffix
	srcA := "source-a-" + suffix
	srcB := "source-b-" + suffix

	var modelID int
	if err := db.QueryRow(ctx,
		`INSERT INTO models (name, slug, provider, modality) VALUES ($1, $2, 'test', 'text') RETURNING id`,
		"test model "+suffix, slug,
	).Scan(&modelID); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	var srcAID int
	if err := db.QueryRow(ctx,
		`INSERT INTO sources (name, url, type) VALUES ($1, 'https://test.example.com', 'api') RETURNING id`, srcA,
	).Scan(&srcAID); err != nil {
		t.Fatalf("insert source A: %v", err)
	}

	var srcBID int
	if err := db.QueryRow(ctx,
		`INSERT INTO sources (name, url, type) VALUES ($1, 'https://test.example.com', 'api') RETURNING id`, srcB,
	).Scan(&srcBID); err != nil {
		t.Fatalf("insert source B: %v", err)
	}

	// Seed initial prices for both sources so LookupCurrentPrice returns
	// non-zero values for the "other" field. Without this, the zero-price
	// guard in publish() rejects every record because the unchanged field is 0.
	for _, sid := range []int{srcAID, srcBID} {
		if _, err := db.Exec(ctx,
			`INSERT INTO prices (model_id, source_id, input_cost_per_token, output_cost_per_token, confidence)
			 VALUES ($1, $2, 0.000001, 0.000001, 'medium')`,
			modelID, sid,
		); err != nil {
			t.Fatalf("seed prices for source %d: %v", sid, err)
		}
	}

	t.Cleanup(func() {
		// Delete in FK-safe order. Log (not Fatal) so cleanup failures are visible
		// without masking the original test result.
		for _, q := range []string{
			`DELETE FROM review_queue  WHERE model_id = $1`,
			`DELETE FROM price_history WHERE model_id = $1`,
			`DELETE FROM prices        WHERE model_id = $1`,
			`DELETE FROM models        WHERE id = $1`,
		} {
			if _, err := db.Exec(ctx, q, modelID); err != nil {
				t.Logf("cleanup warning — %s (model_id=%d): %v", q, modelID, err)
			}
		}
		if _, err := db.Exec(ctx, `DELETE FROM sources WHERE id IN ($1, $2)`, srcAID, srcBID); err != nil {
			t.Logf("cleanup warning — DELETE FROM sources (%d, %d): %v", srcAID, srcBID, err)
		}
	})

	return fixture{
		modelID: modelID,
		srcAID:  srcAID,
		srcBID:  srcBID,
		slug:    slug,
		srcA:    srcA,
		srcB:    srcB,
	}
}

// countRows is a helper that queries a COUNT(*) with a single integer argument
// and returns the result.
func countRows(t *testing.T, db *pgxpool.Pool, query string, arg int) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, arg).Scan(&n); err != nil {
		t.Fatalf("count query %q failed: %v", query, err)
	}
	return n
}

// TestIntegration_TwoSourceAgreement_PublishesToPriceHistory verifies that
// when two sources report the same price, Reconcile inserts exactly one row
// into price_history and upserts one row into prices.
func TestIntegration_TwoSourceAgreement_PublishesToPriceHistory(t *testing.T) {
	db := testDB(t)
	fix := insertFixture(t, db)
	ctx := context.Background()

	diffs := []diff.PriceDiff{
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000005,
			Source:    fix.srcA,
		},
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000005,
			Source:    fix.srcB,
		},
	}

	r := reconciler.New(db)
	if err := r.Reconcile(ctx, diffs); err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	histCount := countRows(t, db,
		`SELECT COUNT(*) FROM price_history WHERE model_id = $1`, fix.modelID)
	if histCount != 1 {
		t.Errorf("expected 1 price_history row after 2-source agreement, got %d", histCount)
	}

	// The prices table has a UNIQUE(model_id, source_id) constraint. We seeded
	// one row per source in insertFixture, and PublishPrice upserts — so after
	// two-source agreement both rows are updated, giving us 2 rows total.
	priceCount := countRows(t, db,
		`SELECT COUNT(*) FROM prices WHERE model_id = $1`, fix.modelID)
	if priceCount != 2 {
		t.Errorf("expected 2 prices rows after 2-source agreement (one per source), got %d", priceCount)
	}
}

// TestIntegration_SingleSource_FirstCycle_NotInPriceHistory verifies that a
// single-source diff on the first cycle is held in the pending map and does
// not produce any price_history rows.
func TestIntegration_SingleSource_FirstCycle_NotInPriceHistory(t *testing.T) {
	db := testDB(t)
	fix := insertFixture(t, db)
	ctx := context.Background()

	diffs := []diff.PriceDiff{
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000005,
			Source:    fix.srcA,
		},
	}

	r := reconciler.New(db)
	if err := r.Reconcile(ctx, diffs); err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	histCount := countRows(t, db,
		`SELECT COUNT(*) FROM price_history WHERE model_id = $1`, fix.modelID)
	if histCount != 0 {
		t.Errorf("expected 0 price_history rows after single-source first cycle, got %d", histCount)
	}
}

// TestIntegration_SingleSource_SecondCycle_InsertedInPriceHistory verifies
// that after a single source reports the same price value in two consecutive
// Reconcile calls, the price is auto-published and appears in price_history.
func TestIntegration_SingleSource_SecondCycle_InsertedInPriceHistory(t *testing.T) {
	db := testDB(t)
	fix := insertFixture(t, db)
	ctx := context.Background()

	d := diff.PriceDiff{
		ModelSlug: fix.slug,
		Field:     models.PriceFieldInput,
		OldValue:  0,
		NewValue:  0.000005,
		Source:    fix.srcA,
	}

	r := reconciler.New(db)

	// Cycle 1: goes to pending, no DB write.
	if err := r.Reconcile(ctx, []diff.PriceDiff{d}); err != nil {
		t.Fatalf("cycle 1 Reconcile error: %v", err)
	}
	histCount := countRows(t, db,
		`SELECT COUNT(*) FROM price_history WHERE model_id = $1`, fix.modelID)
	if histCount != 0 {
		t.Errorf("after cycle 1: expected 0 price_history rows, got %d", histCount)
	}

	// Cycle 2: same value again → auto-publish.
	if err := r.Reconcile(ctx, []diff.PriceDiff{d}); err != nil {
		t.Fatalf("cycle 2 Reconcile error: %v", err)
	}
	histCount = countRows(t, db,
		`SELECT COUNT(*) FROM price_history WHERE model_id = $1`, fix.modelID)
	if histCount != 1 {
		t.Errorf("after cycle 2: expected 1 price_history row, got %d", histCount)
	}
}

// TestIntegration_TwoSourcesDisagree_FlagsReviewQueue verifies that when two
// sources report prices that differ by more than 5%, Reconcile inserts a row
// into review_queue and does NOT write to price_history.
func TestIntegration_TwoSourcesDisagree_FlagsReviewQueue(t *testing.T) {
	db := testDB(t)
	fix := insertFixture(t, db)
	ctx := context.Background()

	// 0.000005 vs 0.000010 → 100% difference, well above the 5% threshold.
	diffs := []diff.PriceDiff{
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000005,
			Source:    fix.srcA,
		},
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000010,
			Source:    fix.srcB,
		},
	}

	r := reconciler.New(db)
	if err := r.Reconcile(ctx, diffs); err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	queueCount := countRows(t, db,
		`SELECT COUNT(*) FROM review_queue WHERE model_id = $1`, fix.modelID)
	if queueCount != 1 {
		t.Errorf("expected 1 review_queue row after >5%% disagreement, got %d", queueCount)
	}

	histCount := countRows(t, db,
		`SELECT COUNT(*) FROM price_history WHERE model_id = $1`, fix.modelID)
	if histCount != 0 {
		t.Errorf("expected 0 price_history rows when sources disagree >5%%, got %d", histCount)
	}
}

// TestIntegration_DuplicateReviewQueueInsert_IsIdempotent verifies that
// calling Reconcile twice with the same disagreeing diffs produces only a
// single review_queue row (ON CONFLICT DO NOTHING on the partial unique index
// WHERE status = 'pending' makes the second insert a no-op).
func TestIntegration_DuplicateReviewQueueInsert_IsIdempotent(t *testing.T) {
	db := testDB(t)
	fix := insertFixture(t, db)
	ctx := context.Background()

	// 0.000005 vs 0.000010 → 100% difference, well above the 5% threshold.
	diffs := []diff.PriceDiff{
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000005,
			Source:    fix.srcA,
		},
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000010,
			Source:    fix.srcB,
		},
	}

	r := reconciler.New(db)

	// First Reconcile — inserts the review_queue row.
	if err := r.Reconcile(ctx, diffs); err != nil {
		t.Fatalf("first Reconcile error: %v", err)
	}

	// Second Reconcile — the ON CONFLICT DO NOTHING partial unique index must
	// suppress the duplicate insert, so count stays at 1.
	if err := r.Reconcile(ctx, diffs); err != nil {
		t.Fatalf("second Reconcile error: %v", err)
	}

	queueCount := countRows(t, db,
		`SELECT COUNT(*) FROM review_queue WHERE model_id = $1`, fix.modelID)
	if queueCount != 1 {
		t.Errorf("expected exactly 1 review_queue row after two identical disagreeing Reconcile calls, got %d", queueCount)
	}
}

// testRedis creates a Redis client from REDIS_URL. Skips the test if the
// env var is absent or Redis is unreachable.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set; skipping Redis integration test")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Skipf("invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Skipf("Redis not reachable: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// TestIntegration_PublishEventEndToEnd verifies that a confirmed price change
// produced by Reconcile() is written to the Redis replay buffer sorted set and
// contains the expected model_id and price fields.
func TestIntegration_PublishEventEndToEnd(t *testing.T) {
	db := testDB(t)
	rdb := testRedis(t)
	fix := insertFixture(t, db)
	ctx := context.Background()

	// Clean up Redis buffer keys created by this test.
	const bufferKey = "price:changes:buffer"
	const seqKey = "price:changes:seq"
	t.Cleanup(func() {
		_ = rdb.Del(context.Background(), bufferKey, seqKey)
	})

	// Flush any pre-existing buffer entries to keep assertions clean.
	_ = rdb.Del(ctx, bufferKey, seqKey)

	diffs := []diff.PriceDiff{
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000009,
			Source:    fix.srcA,
		},
		{
			ModelSlug: fix.slug,
			Field:     models.PriceFieldInput,
			OldValue:  0,
			NewValue:  0.000009,
			Source:    fix.srcB,
		},
	}

	r := reconciler.New(db)
	r.SetRedisClient(rdb)

	if err := r.Reconcile(ctx, diffs); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Verify the event was written to the replay buffer.
	members, err := rdb.ZRange(ctx, bufferKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRANGE %s: %v", bufferKey, err)
	}
	if len(members) == 0 {
		t.Fatal("expected at least one event in the replay buffer after confirmed publish")
	}

	// Decode and verify the event payload.
	var event struct {
		ModelID       int     `json:"model_id"`
		NewPriceInput float64 `json:"new_price_input"`
		EventID       int64   `json:"event_id"`
	}
	if err := json.Unmarshal([]byte(members[0]), &event); err != nil {
		t.Fatalf("unmarshal event JSON: %v", err)
	}
	if event.ModelID != fix.modelID {
		t.Errorf("expected model_id=%d, got %d", fix.modelID, event.ModelID)
	}
	if event.NewPriceInput != 0.000009 {
		t.Errorf("expected new_price_input=0.000009, got %g", event.NewPriceInput)
	}
	if event.EventID < 1 {
		t.Errorf("expected event_id >= 1, got %d", event.EventID)
	}
}
