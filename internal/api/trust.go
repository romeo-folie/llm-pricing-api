package api

import (
	"time"

	"llm-pricing-api/internal/models"
)

// PriceHistoryRow is a minimal view of a price_history record used for trust
// computation. Handlers pass []PriceHistoryRow already fetched from the DB —
// ComputeTrustMeta performs no additional queries.
//
// The field names mirror models.PriceHistory exactly so callers can convert
// cheaply:
//
//	rows := make([]api.PriceHistoryRow, len(history))
//	for i, h := range history {
//	    rows[i] = api.PriceHistoryRow(h)  // requires identical layout
//	}
//
// Because models.PriceHistory uses Source as a source ID (int) and we need a
// string name, handlers must resolve source names before building the slice.
type PriceHistoryRow struct {
	// InputCostPerToken is the per-token cost for prompt tokens.
	InputCostPerToken float64
	// OutputCostPerToken is the per-token cost for completion tokens.
	OutputCostPerToken float64
	// Source is the human-readable source name (e.g. "openrouter", "litellm").
	Source string
	// ConfirmedAt is the timestamp at which the price was confirmed and published.
	ConfirmedAt time.Time
	// RecordedAt is the timestamp at which the record was written to price_history.
	RecordedAt time.Time
}

// TrustMeta holds the trust/quality metadata that accompanies every API response.
// Every field is included in the JSON response envelope under the "meta" key.
type TrustMeta struct {
	// ConfirmedAt is the timestamp of the most recent confirmed price record.
	ConfirmedAt time.Time `json:"confirmed_at"`
	// Source is the name of the primary source for the latest value.
	Source string `json:"source"`
	// Confidence is "high", "medium", or "low" — see ComputeTrustMeta for rules.
	Confidence models.Confidence `json:"confidence"`
	// AgeHours is how many hours have elapsed since the latest confirmation.
	AgeHours float64 `json:"age_hours"`
	// ChangeVelocity is the number of distinct price values in the last 30 days
	// divided by 30 — a measure of how rapidly the price is changing.
	ChangeVelocity float64 `json:"change_velocity"`
}

// ComputeTrustMeta derives a TrustMeta value from a slice of price history rows.
// It is a pure function: no I/O, no side-effects, fully deterministic.
//
// Confidence rules:
//   - "high"   — 2+ distinct source names agree on the latest (input, output) price pair.
//   - "medium" — single source, confirmed within the last 24 hours.
//   - "low"    — single source, confirmed more than 24 hours ago.
//
// If rows is empty, ComputeTrustMeta returns a zero-value TrustMeta with
// Confidence="low" to signal that no verified data is available.
func ComputeTrustMeta(rows []PriceHistoryRow) TrustMeta {
	if len(rows) == 0 {
		return TrustMeta{Confidence: models.ConfidenceLow}
	}

	// --- find the latest record -----------------------------------------
	latest := rows[0]
	for _, r := range rows[1:] {
		if r.ConfirmedAt.After(latest.ConfirmedAt) {
			latest = r
		}
	}

	now := time.Now().UTC()
	ageHours := now.Sub(latest.ConfirmedAt).Hours()

	// --- collect records that share the same (input, output) price pair --
	// We compare prices using the same value so floating-point equality is
	// intentional here; history rows are written from exact DB values.
	type pricePair struct {
		input  float64
		output float64
	}
	latestPair := pricePair{latest.InputCostPerToken, latest.OutputCostPerToken}

	// Gather the set of source names that agree on the latest price pair.
	sourcesForLatest := make(map[string]struct{})
	for _, r := range rows {
		rp := pricePair{r.InputCostPerToken, r.OutputCostPerToken}
		if rp == latestPair {
			sourcesForLatest[r.Source] = struct{}{}
		}
	}

	// --- determine confidence -------------------------------------------
	var confidence models.Confidence
	switch {
	case len(sourcesForLatest) >= 2:
		confidence = models.ConfidenceHigh
	case ageHours <= 24:
		confidence = models.ConfidenceMedium
	default:
		confidence = models.ConfidenceLow
	}

	// --- change velocity ------------------------------------------------
	// Count distinct price-pair values within the last 30 days, then
	// divide by 30 to express velocity as "changes per day".
	cutoff := now.AddDate(0, 0, -30)
	distinctValues := make(map[pricePair]struct{})
	for _, r := range rows {
		if r.ConfirmedAt.After(cutoff) {
			distinctValues[pricePair{r.InputCostPerToken, r.OutputCostPerToken}] = struct{}{}
		}
	}
	changeVelocity := float64(len(distinctValues)) / 30.0

	return TrustMeta{
		ConfirmedAt:    latest.ConfirmedAt,
		Source:         latest.Source,
		Confidence:     confidence,
		AgeHours:       ageHours,
		ChangeVelocity: changeVelocity,
	}
}
