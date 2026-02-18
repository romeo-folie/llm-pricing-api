package models

import "time"

// DiscrepancyThreshold is the minimum relative price difference between two
// sources that triggers a review-queue flag or a NeedsReview annotation on a
// PriceDiff. Both the diff engine and the reconciler import this constant so
// the business rule is defined in exactly one place.
const DiscrepancyThreshold = 0.05

// Confidence represents how certain we are about a price value,
// matching the CHECK constraint on the prices.confidence column.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// PriceField identifies which price component a diff or review queue entry concerns,
// matching the CHECK constraint on the review_queue.field column.
type PriceField string

const (
	PriceFieldInput  PriceField = "input_cost_per_token"
	PriceFieldOutput PriceField = "output_cost_per_token"
)

// Price mirrors the prices table — the current confirmed price for a model
// from a given source. There is at most one row per (model_id, source_id) pair.
type Price struct {
	ID                 int
	ModelID            int
	InputCostPerToken  float64
	OutputCostPerToken float64
	SourceID           int
	ConfirmedAt        time.Time
	Confidence         Confidence
	CreatedAt          time.Time
}

// PriceHistory mirrors the price_history hypertable — an immutable record
// of every confirmed price change. Rows are never updated or deleted.
type PriceHistory struct {
	ModelID            int
	InputCostPerToken  float64
	OutputCostPerToken float64
	SourceID           int
	ConfirmedAt        time.Time
	RecordedAt         time.Time
}

// ReviewStatus represents the current state of a review queue entry,
// matching the CHECK constraint on the review_queue.status column.
type ReviewStatus string

const (
	ReviewStatusPending    ReviewStatus = "pending"
	ReviewStatusResolved   ReviewStatus = "resolved"
	ReviewStatusOverridden ReviewStatus = "overridden"
)

// ReviewQueueItem mirrors the review_queue table — a flagged price discrepancy
// awaiting operator approval or rejection.
type ReviewQueueItem struct {
	ID         int
	ModelID    int
	SourceAID  int
	SourceBID  int
	Field      PriceField
	ValueA     float64
	ValueB     float64
	DeltaPct   float64
	Status     ReviewStatus
	CreatedAt  time.Time
	ResolvedAt *time.Time // nil until actioned
}
