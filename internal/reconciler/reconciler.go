package reconciler

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/models"
)

const typeWebhookDeliver = "webhook:deliver"

// webhookPayload is the event body sent to registered webhook URLs.
type webhookPayload struct {
	ModelID        int       `json:"model_id"`
	Provider       string    `json:"provider"`
	OldPriceInput  float64   `json:"old_price_input"`
	OldPriceOutput float64   `json:"old_price_output"`
	NewPriceInput  float64   `json:"new_price_input"`
	NewPriceOutput float64   `json:"new_price_output"`
	ConfirmedAt    time.Time `json:"confirmed_at"`
	Source         string    `json:"source"`
}

// webhookTaskPayload is the asynq task payload for webhook delivery.
type webhookTaskPayload struct {
	WebhookID string         `json:"webhook_id"`
	URL       string         `json:"url"`
	Secret    string         `json:"secret"`
	Event     webhookPayload `json:"event"`
}

// newWebhookDeliverTask builds an asynq.Task for TypeWebhookDeliver.
func newWebhookDeliverTask(p webhookTaskPayload) (*asynq.Task, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook task payload: %w", err)
	}
	return asynq.NewTask(typeWebhookDeliver,
		data,
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
	), nil
}

const (
	// epsilon is the floating-point tolerance used when comparing prices.
	epsilon = 1e-12

	// pendingTTL is the maximum time a single-source change can sit in the
	// pending map before being swept as stale.  Three days covers the slowest
	// scraper cadence (daily) with a generous safety margin.
	pendingTTL = 72 * time.Hour
)

// pendingChange tracks a single-source price change awaiting a second
// confirming fetch before it can be auto-published.
type pendingChange struct {
	value      float64
	source     string
	fetchCount int
	lastSeen   time.Time // updated every cycle; entries older than pendingTTL are swept
}

// Reconciler mediates all writes to price_history and prices.
//
// It holds an in-memory pending map that persists across Reconcile calls so
// it can detect a single source reporting the same value in two consecutive
// scraper cycles (the 2-consecutive-fetch auto-publish rule).
//
// Reconciler is safe for concurrent use.
type Reconciler struct {
	store       Store
	pending     map[string]*pendingChange // key: slug+":"+field+":"+source
	mu          sync.Mutex
	asynqClient *asynq.Client // optional; nil = webhook delivery disabled
}

// New returns a Reconciler backed by the given PostgreSQL pool.
func New(db *pgxpool.Pool) *Reconciler {
	return NewWithStore(&pgxStore{db: db})
}

// NewWithStore returns a Reconciler that uses the provided Store.
// Intended for use in tests with a mock store.
func NewWithStore(s Store) *Reconciler {
	return &Reconciler{
		store:   s,
		pending: make(map[string]*pendingChange),
	}
}

// SetAsynqClient attaches an asynq.Client to the Reconciler so that confirmed
// price changes fan out webhook delivery tasks.  Calling with nil disables
// webhook delivery (the default).
func (r *Reconciler) SetAsynqClient(c *asynq.Client) {
	r.asynqClient = c
}

// sweepStalePending removes pending entries whose lastSeen time is older than
// pendingTTL.  It is called at the start of each Reconcile cycle to keep the
// in-memory map bounded even when scrapers stop reporting a slug+field.
func (r *Reconciler) sweepStalePending() {
	cutoff := time.Now().Add(-pendingTTL)
	r.mu.Lock()
	for k, e := range r.pending {
		if e.lastSeen.Before(cutoff) {
			delete(r.pending, k)
		}
	}
	r.mu.Unlock()
}

// Reconcile applies a batch of price diffs produced by the diff engine.
//
// Multi-source groups (same slug+field, multiple sources):
//   - All agree within epsilon → publish with ConfidenceHigh
//   - Consensus cluster of 2+ sources, outlier >5% → flag AND publish ConfidenceHigh
//   - No consensus (all disagree), max delta >5% → flag for review, no publish
//   - All close but not exactly equal (<5% spread) → publish with ConfidenceMedium
//
// Single-source groups:
//   - First occurrence → track in pending map
//   - Same value seen again in next cycle → auto-publish with ConfidenceMedium
//   - Value changed between cycles → reset counter, stay pending
//
// Errors from individual store operations are logged and swallowed so that
// a DB hiccup affecting one model does not abort reconciliation for all others.
// Reconcile itself only returns a non-nil error for catastrophic failures
// (currently none — all per-model errors are demoted to warnings).
func (r *Reconciler) Reconcile(ctx context.Context, diffs []diff.PriceDiff) error {
	if len(diffs) == 0 {
		return nil
	}

	r.sweepStalePending()
	slog.Info("reconciler: starting", "num_diffs", len(diffs))

	// --- resolve source IDs (cached for this cycle to avoid N+1 queries) ---
	sourceIDs := make(map[string]int, len(diffs))
	for _, d := range diffs {
		if _, seen := sourceIDs[d.Source]; seen {
			continue
		}
		id, err := r.store.LookupSourceID(ctx, d.Source)
		if err != nil {
			slog.Warn("reconciler: unknown source — skipping its diffs", "source", d.Source, "err", err)
			sourceIDs[d.Source] = -1 // sentinel: invalid
			continue
		}
		sourceIDs[d.Source] = id
	}

	// --- group valid diffs by (slug, field) ---
	type groupKey struct {
		slug  string
		field models.PriceField
	}
	groups := make(map[groupKey][]diff.PriceDiff)
	for _, d := range diffs {
		if sourceIDs[d.Source] < 0 {
			continue // drop diffs from unresolvable sources
		}
		k := groupKey{d.ModelSlug, d.Field}
		groups[k] = append(groups[k], d)
	}

	// --- process each group ---
	// processMultiSource does DB I/O only — no lock needed (no shared state).
	// processSingleSource acquires the lock internally when mutating r.pending.
	for k, groupDiffs := range groups {
		modelID, err := r.store.LookupModelID(ctx, k.slug)
		if err != nil {
			slog.Warn("reconciler: unknown model — skipping", "slug", k.slug, "err", err)
			continue
		}

		if len(groupDiffs) >= 2 {
			r.processMultiSource(ctx, k.slug, k.field, modelID, groupDiffs, sourceIDs)
		} else {
			r.processSingleSource(ctx, k.slug, k.field, modelID, groupDiffs[0], sourceIDs)
		}
	}

	r.mu.Lock()
	pendingCount := len(r.pending)
	r.mu.Unlock()
	slog.Info("reconciler: done", "pending_entries", pendingCount)
	return nil
}

// processMultiSource handles a group of 2+ diffs for the same (slug, field).
//
// Decision tree:
//  1. All sources agree within epsilon → publish ConfidenceHigh.
//  2. A consensus cluster of 2+ sources exists AND an outlier differs by >5%
//     → flag the worst pair AND publish the consensus value with ConfidenceHigh.
//     (A single noisy source does not block publishing when the majority agrees.)
//  3. No consensus cluster (all sources disagree pairwise) AND max delta >5%
//     → flag for review, no publish.
//  4. All sources within 5% spread (minor disagreement) → publish ConfidenceMedium.
func (r *Reconciler) processMultiSource(
	ctx context.Context,
	slug string,
	field models.PriceField,
	modelID int,
	groupDiffs []diff.PriceDiff,
	sourceIDs map[string]int,
) {
	type valuediff struct {
		value    float64
		sourceID int
	}

	// Sort by source name for deterministic attribution in price_history.
	slices.SortFunc(groupDiffs, func(a, b diff.PriceDiff) int {
		return cmp.Compare(a.Source, b.Source)
	})

	values := make([]valuediff, 0, len(groupDiffs))
	for _, d := range groupDiffs {
		values = append(values, valuediff{d.NewValue, sourceIDs[d.Source]})
	}

	// Fast path: all sources report the same value within floating-point epsilon.
	ref := values[0].value
	allAgree := true
	for _, v := range values[1:] {
		if math.Abs(v.value-ref) > epsilon {
			allAgree = false
			break
		}
	}
	if allAgree {
		r.publish(ctx, modelID, values[0].sourceID, field, ref, models.ConfidenceHigh)
		return
	}

	// Find the largest consensus cluster: sources whose values agree within epsilon.
	// O(n²), but n is bounded by the number of data sources (typically 3–7).
	consensusSize := 0
	consensusValue := 0.0
	consensusSourceID := 0
	for i := 0; i < len(values); i++ {
		size := 0
		for j := 0; j < len(values); j++ {
			if math.Abs(values[j].value-values[i].value) <= epsilon {
				size++
			}
		}
		if size > consensusSize {
			consensusSize = size
			consensusValue = values[i].value
			consensusSourceID = values[i].sourceID
		}
	}

	// Find the pair with the largest relative disagreement (used for flagging).
	maxDelta := 0.0
	var valueA, valueB float64
	var srcA, srcB int
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			hi := math.Max(values[i].value, values[j].value)
			if hi < epsilon {
				continue
			}
			delta := math.Abs(values[i].value-values[j].value) / hi
			if delta > maxDelta {
				maxDelta = delta
				valueA, valueB = values[i].value, values[j].value
				srcA, srcB = values[i].sourceID, values[j].sourceID
			}
		}
	}

	if maxDelta > models.DiscrepancyThreshold {
		slog.Warn("reconciler: flagging discrepancy",
			"slug", slug, "field", field,
			"value_a", valueA, "value_b", valueB,
			"delta_pct", maxDelta)
		if err := r.store.FlagDiscrepancy(ctx, modelID, srcA, srcB, field, valueA, valueB, maxDelta); err != nil {
			// Duplicate pending review (DB ON CONFLICT DO NOTHING) surfaces as nil, but
			// any other store error is demoted to a warning so it doesn't abort the cycle.
			slog.Warn("reconciler: FlagDiscrepancy failed", "err", err)
		}
		// If a consensus of 2+ sources exists, publish despite the outlier.
		// This implements the PRD rule: single noisy source ≠ block on majority agreement.
		if consensusSize >= 2 {
			r.publish(ctx, modelID, consensusSourceID, field, consensusValue, models.ConfidenceHigh)
		}
		return
	}

	// Minor disagreement (≤5%): all sources broadly agree, publish with reduced confidence.
	// We publish `ref` (values[0].value) — the value from the first source alphabetically
	// after the sort above.  All candidates are within 5% of each other, so any value is
	// a valid representation; alphabetical determinism is intentional and keeps the output
	// stable across re-runs with the same source set.
	r.publish(ctx, modelID, values[0].sourceID, field, ref, models.ConfidenceMedium)
}

// processSingleSource handles a group where only one source reported a change.
// It tracks the change in the pending map and publishes on the second consecutive cycle.
// The mutex is acquired only while accessing the pending map; DB calls happen outside it.
func (r *Reconciler) processSingleSource(
	ctx context.Context,
	slug string,
	field models.PriceField,
	modelID int,
	d diff.PriceDiff,
	sourceIDs map[string]int,
) {
	key := slug + ":" + string(field) + ":" + d.Source
	sourceID := sourceIDs[d.Source]

	r.mu.Lock()
	entry, ok := r.pending[key]
	shouldPublish := false
	if ok && math.Abs(entry.value-d.NewValue) < epsilon {
		entry.fetchCount++
		entry.lastSeen = time.Now()
		if entry.fetchCount >= 2 {
			shouldPublish = true
			delete(r.pending, key)
		}
	} else {
		// New value (or first time seeing this slug+field+source): start/reset counter.
		r.pending[key] = &pendingChange{value: d.NewValue, source: d.Source, fetchCount: 1, lastSeen: time.Now()}
	}
	r.mu.Unlock()

	if shouldPublish {
		r.publish(ctx, modelID, sourceID, field, d.NewValue, models.ConfidenceMedium)
	}
}

// publish writes a confirmed price event to the database.
// It reads the current price row first so the unchanged field value is preserved
// in the price_history snapshot (both columns are NOT NULL).
// After a successful write, if an asynq client is configured, it fans out
// webhook delivery tasks to all active registrations.
func (r *Reconciler) publish(
	ctx context.Context,
	modelID, sourceID int,
	field models.PriceField,
	newValue float64,
	confidence models.Confidence,
) {
	currInput, currOutput, _, err := r.store.LookupCurrentPrice(ctx, modelID, sourceID)
	if err != nil {
		slog.Warn("reconciler: LookupCurrentPrice failed; unchanged field will be 0",
			"model_id", modelID, "source_id", sourceID, "err", err)
	}

	var input, output float64
	switch field {
	case models.PriceFieldInput:
		input = newValue
		output = currOutput
	case models.PriceFieldOutput:
		input = currInput
		output = newValue
	default:
		slog.Error("reconciler: unknown PriceField; skipping publish to prevent data corruption",
			"field", field, "model_id", modelID, "source_id", sourceID)
		return
	}

	if err := r.store.PublishPrice(ctx, modelID, sourceID, input, output, confidence); err != nil {
		slog.Warn("reconciler: PublishPrice failed",
			"model_id", modelID, "source_id", sourceID, "err", err)
		return
	}

	if r.asynqClient != nil {
		r.enqueueWebhooks(ctx, modelID, sourceID, currInput, currOutput, input, output)
	}
}

// enqueueWebhooks fans out a webhook:deliver task to every active webhook registration.
// Errors are logged but never returned — a delivery failure must not abort reconciliation.
func (r *Reconciler) enqueueWebhooks(
	ctx context.Context,
	modelID, sourceID int,
	oldInput, oldOutput float64,
	newInput, newOutput float64,
) {
	webhooks, err := r.store.ListActiveWebhooks(ctx)
	if err != nil {
		slog.Warn("reconciler: ListActiveWebhooks failed; skipping webhook fan-out",
			"model_id", modelID, "err", err)
		return
	}
	if len(webhooks) == 0 {
		return
	}

	sourceName, err := r.store.LookupSourceName(ctx, sourceID)
	if err != nil {
		slog.Warn("reconciler: LookupSourceName failed; using empty string in webhook payload",
			"source_id", sourceID, "err", err)
	}

	provider, err := r.store.LookupModelProvider(ctx, modelID)
	if err != nil {
		slog.Warn("reconciler: LookupModelProvider failed; using empty string in webhook payload",
			"model_id", modelID, "err", err)
	}

	event := webhookPayload{
		ModelID:        modelID,
		Provider:       provider,
		OldPriceInput:  oldInput,
		OldPriceOutput: oldOutput,
		NewPriceInput:  newInput,
		NewPriceOutput: newOutput,
		ConfirmedAt:    time.Now().UTC(),
		Source:         sourceName,
	}

	for _, wh := range webhooks {
		task, err := newWebhookDeliverTask(webhookTaskPayload{
			WebhookID: wh.ID,
			URL:       wh.URL,
			Secret:    wh.Secret,
			Event:     event,
		})
		if err != nil {
			slog.Warn("reconciler: failed to build webhook task",
				"webhook_id", wh.ID, "err", err)
			continue
		}
		if _, err := r.asynqClient.EnqueueContext(ctx, task); err != nil {
			slog.Warn("reconciler: failed to enqueue webhook delivery",
				"webhook_id", wh.ID, "err", err)
		}
	}
}
