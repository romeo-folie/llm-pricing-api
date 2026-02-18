package api_test

import (
	"math"
	"testing"
	"time"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/models"
)

// fixed reference time used across all test cases.
var now = time.Now().UTC()

// TestComputeTrustMeta_Empty verifies that an empty slice returns a zero-value
// TrustMeta with ConfidenceLow.
func TestComputeTrustMeta_Empty(t *testing.T) {
	meta := api.ComputeTrustMeta(nil)
	if meta.Confidence != models.ConfidenceLow {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceLow)
	}
	if !meta.ConfirmedAt.IsZero() {
		t.Errorf("ConfirmedAt: expected zero time, got %v", meta.ConfirmedAt)
	}
}

// TestComputeTrustMeta_EmptySlice covers the empty slice (non-nil) case.
func TestComputeTrustMeta_EmptySlice(t *testing.T) {
	meta := api.ComputeTrustMeta([]api.PriceHistoryRow{})
	if meta.Confidence != models.ConfidenceLow {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceLow)
	}
}

// TestComputeTrustMeta_HighConfidence verifies that 2+ distinct sources
// agreeing on the latest price produces ConfidenceHigh.
func TestComputeTrustMeta_HighConfidence(t *testing.T) {
	confirmedAt := now.Add(-1 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "litellm", ConfirmedAt: confirmedAt.Add(-30 * time.Minute), RecordedAt: confirmedAt},
	}

	meta := api.ComputeTrustMeta(rows)

	if meta.Confidence != models.ConfidenceHigh {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceHigh)
	}
	if meta.Source != "openrouter" {
		t.Errorf("Source: got %q, want %q", meta.Source, "openrouter")
	}
	if !almostEqual(meta.ConfirmedAt.Unix(), confirmedAt.Unix(), 1) {
		t.Errorf("ConfirmedAt: got %v, want %v", meta.ConfirmedAt, confirmedAt)
	}
}

// TestComputeTrustMeta_MediumConfidence_Recent verifies that a single source
// confirmed within 24h produces ConfidenceMedium.
func TestComputeTrustMeta_MediumConfidence_Recent(t *testing.T) {
	confirmedAt := now.Add(-12 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
	}

	meta := api.ComputeTrustMeta(rows)

	if meta.Confidence != models.ConfidenceMedium {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceMedium)
	}
	if meta.Source != "openrouter" {
		t.Errorf("Source: got %q, want %q", meta.Source, "openrouter")
	}
	if meta.AgeHours < 11 || meta.AgeHours > 13 {
		t.Errorf("AgeHours: got %f, expected ~12", meta.AgeHours)
	}
}

// TestComputeTrustMeta_LowConfidence_OldSingle verifies that a single source
// confirmed more than 24h ago produces ConfidenceLow.
func TestComputeTrustMeta_LowConfidence_OldSingle(t *testing.T) {
	confirmedAt := now.Add(-48 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.003, OutputCostPerToken: 0.006, Source: "litellm", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
	}

	meta := api.ComputeTrustMeta(rows)

	if meta.Confidence != models.ConfidenceLow {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceLow)
	}
	if meta.AgeHours < 47 || meta.AgeHours > 49 {
		t.Errorf("AgeHours: got %f, expected ~48", meta.AgeHours)
	}
}

// TestComputeTrustMeta_LatestRowChosen verifies that the row with the latest
// ConfirmedAt is selected as the authoritative source.
func TestComputeTrustMeta_LatestRowChosen(t *testing.T) {
	older := now.Add(-10 * time.Hour)
	newer := now.Add(-2 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "litellm", ConfirmedAt: older, RecordedAt: older},
		{InputCostPerToken: 0.0015, OutputCostPerToken: 0.003, Source: "openrouter", ConfirmedAt: newer, RecordedAt: newer},
	}

	meta := api.ComputeTrustMeta(rows)

	// openrouter is the latest, so it should be the source.
	if meta.Source != "openrouter" {
		t.Errorf("Source: got %q, want %q", meta.Source, "openrouter")
	}
}

// TestComputeTrustMeta_ChangeVelocity_SingleValue verifies that a single
// distinct price in the last 30 days yields velocity = 1/30.
func TestComputeTrustMeta_ChangeVelocity_SingleValue(t *testing.T) {
	confirmedAt := now.Add(-1 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
	}

	meta := api.ComputeTrustMeta(rows)

	expected := 1.0 / 30.0
	if math.Abs(meta.ChangeVelocity-expected) > 1e-9 {
		t.Errorf("ChangeVelocity: got %f, want %f", meta.ChangeVelocity, expected)
	}
}

// TestComputeTrustMeta_ChangeVelocity_MultipleValues verifies that multiple
// distinct price values in the last 30 days are counted correctly.
func TestComputeTrustMeta_ChangeVelocity_MultipleValues(t *testing.T) {
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: now.Add(-1 * time.Hour), RecordedAt: now.Add(-1 * time.Hour)},
		{InputCostPerToken: 0.002, OutputCostPerToken: 0.004, Source: "openrouter", ConfirmedAt: now.Add(-5 * time.Hour), RecordedAt: now.Add(-5 * time.Hour)},
		{InputCostPerToken: 0.003, OutputCostPerToken: 0.006, Source: "openrouter", ConfirmedAt: now.Add(-10 * time.Hour), RecordedAt: now.Add(-10 * time.Hour)},
	}

	meta := api.ComputeTrustMeta(rows)

	expected := 3.0 / 30.0
	if math.Abs(meta.ChangeVelocity-expected) > 1e-9 {
		t.Errorf("ChangeVelocity: got %f, want %f", meta.ChangeVelocity, expected)
	}
}

// TestComputeTrustMeta_ChangeVelocity_OldRecordsExcluded verifies that rows
// older than 30 days are excluded from the velocity calculation.
func TestComputeTrustMeta_ChangeVelocity_OldRecordsExcluded(t *testing.T) {
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: now.Add(-1 * time.Hour), RecordedAt: now.Add(-1 * time.Hour)},
		// Older than 30 days — should be excluded from velocity.
		{InputCostPerToken: 0.005, OutputCostPerToken: 0.010, Source: "openrouter", ConfirmedAt: now.AddDate(0, 0, -31), RecordedAt: now.AddDate(0, 0, -31)},
	}

	meta := api.ComputeTrustMeta(rows)

	// Only 1 distinct value within 30 days.
	expected := 1.0 / 30.0
	if math.Abs(meta.ChangeVelocity-expected) > 1e-9 {
		t.Errorf("ChangeVelocity: got %f, want %f (old record should be excluded)", meta.ChangeVelocity, expected)
	}
}

// TestComputeTrustMeta_DuplicateSourcesNotDoubleCount verifies that the same
// price value from multiple rows of the same source only counts as one distinct value.
func TestComputeTrustMeta_DuplicateSourcesNotDoubleCount(t *testing.T) {
	confirmedAt := now.Add(-1 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: confirmedAt.Add(-1 * time.Hour), RecordedAt: confirmedAt},
	}

	meta := api.ComputeTrustMeta(rows)

	// Two rows with the same price pair from the same source.
	// Distinct values = 1, so velocity = 1/30.
	expected := 1.0 / 30.0
	if math.Abs(meta.ChangeVelocity-expected) > 1e-9 {
		t.Errorf("ChangeVelocity: got %f, want %f", meta.ChangeVelocity, expected)
	}
	// Same price pair from same source: only 1 source entry for the latest value.
	// ConfirmedAt should be the most recent.
	if meta.Confidence != models.ConfidenceMedium {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceMedium)
	}
}

// TestComputeTrustMeta_HighConfidence_TwoSourcesSamePrice verifies the
// "2+ distinct sources agree" rule in the high confidence path.
func TestComputeTrustMeta_HighConfidence_TwoSourcesSamePrice(t *testing.T) {
	confirmedAt := now.Add(-2 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.005, OutputCostPerToken: 0.010, Source: "openrouter", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
		{InputCostPerToken: 0.005, OutputCostPerToken: 0.010, Source: "litellm", ConfirmedAt: confirmedAt.Add(-30 * time.Minute), RecordedAt: confirmedAt},
		// Older record with different price — should not affect latest confidence.
		{InputCostPerToken: 0.003, OutputCostPerToken: 0.006, Source: "openrouter", ConfirmedAt: confirmedAt.Add(-5 * time.Hour), RecordedAt: confirmedAt.Add(-5 * time.Hour)},
	}

	meta := api.ComputeTrustMeta(rows)

	if meta.Confidence != models.ConfidenceHigh {
		t.Errorf("Confidence: got %q, want %q", meta.Confidence, models.ConfidenceHigh)
	}
}

// TestComputeTrustMeta_AgeHoursAccuracy verifies AgeHours reflects the time
// since the latest ConfirmedAt.
func TestComputeTrustMeta_AgeHoursAccuracy(t *testing.T) {
	confirmedAt := now.Add(-6 * time.Hour)
	rows := []api.PriceHistoryRow{
		{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, Source: "openrouter", ConfirmedAt: confirmedAt, RecordedAt: confirmedAt},
	}

	meta := api.ComputeTrustMeta(rows)

	// Allow ±0.1 hour (6 minutes) for execution time.
	if math.Abs(meta.AgeHours-6.0) > 0.1 {
		t.Errorf("AgeHours: got %f, want ~6.0", meta.AgeHours)
	}
}

// almostEqual returns true when the two int64 values differ by at most delta.
func almostEqual(a, b, delta int64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= delta
}
