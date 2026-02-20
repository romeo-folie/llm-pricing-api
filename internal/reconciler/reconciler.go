package reconciler

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/webhooks"
)

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
	asynqClient *asynq.Client  // optional; nil = webhook delivery disabled
	redisClient *redis.Client  // optional; nil = Pub/Sub publishing disabled
	logger      zerolog.Logger
}

// New returns a Reconciler backed by the given PostgreSQL pool.
func New(db *pgxpool.Pool) *Reconciler {
	return NewWithStore(&pgxStore{db: db, logger: zerolog.Nop()})
}

// NewWithStore returns a Reconciler that uses the provided Store.
// Intended for use in tests with a mock store.
func NewWithStore(s Store) *Reconciler {
	return &Reconciler{
		store:   s,
		pending: make(map[string]*pendingChange),
		logger:  zerolog.Nop(),
	}
}

// SetAsynqClient attaches an asynq.Client to the Reconciler so that confirmed
// price changes fan out webhook delivery tasks.  Calling with nil disables
// webhook delivery (the default).
func (r *Reconciler) SetAsynqClient(c *asynq.Client) {
	r.asynqClient = c
}

// SetRedisClient attaches a Redis client to the Reconciler so that confirmed
// price changes are published to the price:changes Pub/Sub channel and stored
// in the replay buffer. Calling with nil disables event publishing (the default).
func (r *Reconciler) SetRedisClient(c *redis.Client) {
	r.redisClient = c
}

// SetLogger configures the zerolog.Logger used by the Reconciler and its
// underlying store (if the store supports it). The default is zerolog.Nop().
func (r *Reconciler) SetLogger(l zerolog.Logger) {
	r.logger = l
	// Propagate to the store if it supports logger injection.
	if s, ok := r.store.(*pgxStore); ok {
		s.logger = l
	}
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

// effectiveProvider returns the infrastructure provider identity used for
// independence checking. For pass-through aggregators (HuggingFace, OpenRouter),
// this is the UnderlyingProvider value (e.g. "together-ai"). For direct sources
// (LiteLLM), it falls back to the source name so they are always treated as
// independent from aggregators.
//
// NOTE: independence checks require that both the slug AND the effectiveProvider
// string match. Two sources with the same UnderlyingProvider but different slugs
// are in separate (slug, field) groups and are never compared. This is a V1
// best-effort check; slug normalisation alignment between HuggingFace and OpenRouter
// is handled upstream in the respective scrapers.
func effectiveProvider(d diff.PriceDiff) string {
	if d.UnderlyingProvider != "" {
		return d.UnderlyingProvider
	}
	return d.Source
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
	r.logger.Info().Int("num_diffs", len(diffs)).Msg("reconciler: starting")

	// --- resolve source IDs (cached for this cycle to avoid N+1 queries) ---
	sourceIDs := make(map[string]int, len(diffs))
	for _, d := range diffs {
		if _, seen := sourceIDs[d.Source]; seen {
			continue
		}
		id, err := r.store.LookupSourceID(ctx, d.Source)
		if err != nil {
			r.logger.Warn().Str("source", d.Source).Err(err).Msg("reconciler: unknown source — skipping its diffs")
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

	// Fetch active webhooks once per reconciliation cycle instead of once per
	// publish call to avoid an N+1 query when many models change in the same run.
	// If the fetch fails, we proceed with an empty slice — price writes continue
	// and webhook fan-out is skipped for this cycle only.
	var activeWebhooks []WebhookRow
	if r.asynqClient != nil {
		var whErr error
		activeWebhooks, whErr = r.store.ListActiveWebhooks(ctx)
		if whErr != nil {
			r.logger.Warn().Err(whErr).Msg("reconciler: ListActiveWebhooks failed; skipping webhook fan-out for this cycle")
			activeWebhooks = nil
		}
	}

	// --- process each group ---
	// processMultiSource does DB I/O only — no lock needed (no shared state).
	// processSingleSource acquires the lock internally when mutating r.pending.
	for k, groupDiffs := range groups {
		modelID, err := r.store.LookupModelID(ctx, k.slug)
		if err != nil {
			r.logger.Warn().Str("slug", k.slug).Err(err).Msg("reconciler: unknown model — skipping")
			continue
		}

		if len(groupDiffs) >= 2 {
			// Count distinct underlying infrastructure providers.
			// If all diffs trace back to the same provider (e.g. HuggingFace + OpenRouter
			// both reporting Together AI prices), treat as single-source — they are NOT
			// independent confirmations.
			distinct := make(map[string]struct{}, len(groupDiffs))
			for _, d := range groupDiffs {
				distinct[effectiveProvider(d)] = struct{}{}
			}
			if len(distinct) < 2 {
				r.logger.Debug().
					Str("slug", k.slug).
					Str("field", string(k.field)).
					Int("source_count", len(groupDiffs)).
					Msg("reconciler: collapsing non-independent sources to single-source")
				r.processSingleSource(ctx, k.slug, k.field, modelID, groupDiffs[0], sourceIDs, activeWebhooks)
			} else {
				r.processMultiSource(ctx, k.slug, k.field, modelID, groupDiffs, sourceIDs, activeWebhooks)
			}
		} else {
			r.processSingleSource(ctx, k.slug, k.field, modelID, groupDiffs[0], sourceIDs, activeWebhooks)
		}
	}

	r.mu.Lock()
	pendingCount := len(r.pending)
	r.mu.Unlock()
	r.logger.Info().Int("pending_entries", pendingCount).Msg("reconciler: done")
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
	activeWebhooks []WebhookRow,
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
		r.publish(ctx, modelID, values[0].sourceID, field, ref, models.ConfidenceHigh, groupDiffs[0].UnderlyingProvider, activeWebhooks)
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
		r.logger.Warn().
			Str("slug", slug).
			Str("field", string(field)).
			Float64("value_a", valueA).
			Float64("value_b", valueB).
			Float64("delta_pct", maxDelta).
			Msg("reconciler: flagging discrepancy")
		if err := r.store.FlagDiscrepancy(ctx, modelID, srcA, srcB, field, valueA, valueB, maxDelta); err != nil {
			// Duplicate pending review (DB ON CONFLICT DO NOTHING) surfaces as nil, but
			// any other store error is demoted to a warning so it doesn't abort the cycle.
			r.logger.Warn().Err(err).Msg("reconciler: FlagDiscrepancy failed")
		}
		// If a consensus of 2+ sources exists, publish despite the outlier.
		// This implements the PRD rule: single noisy source ≠ block on majority agreement.
		if consensusSize >= 2 {
			r.publish(ctx, modelID, consensusSourceID, field, consensusValue, models.ConfidenceHigh, groupDiffs[0].UnderlyingProvider, activeWebhooks)
		}
		return
	}

	// Minor disagreement (≤5%): all sources broadly agree, publish with reduced confidence.
	// We publish `ref` (values[0].value) — the value from the first source alphabetically
	// after the sort above.  All candidates are within 5% of each other, so any value is
	// a valid representation; alphabetical determinism is intentional and keeps the output
	// stable across re-runs with the same source set.
	r.publish(ctx, modelID, values[0].sourceID, field, ref, models.ConfidenceMedium, groupDiffs[0].UnderlyingProvider, activeWebhooks)
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
	activeWebhooks []WebhookRow,
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
		r.publish(ctx, modelID, sourceID, field, d.NewValue, models.ConfidenceMedium, d.UnderlyingProvider, activeWebhooks)
	}
}

// publish writes a confirmed price event to the database.
// It reads the current price row first so the unchanged field value is preserved
// in the price_history snapshot (both columns are NOT NULL).
// After a successful write, it resolves source name and provider, then:
//   - fans out webhook delivery tasks (if asynq client is configured and webhooks are registered)
//   - publishes to Redis Pub/Sub and the replay buffer (if a Redis client is configured)
func (r *Reconciler) publish(
	ctx context.Context,
	modelID, sourceID int,
	field models.PriceField,
	newValue float64,
	confidence models.Confidence,
	underlyingProvider string,
	activeWebhooks []WebhookRow,
) {
	currInput, currOutput, _, err := r.store.LookupCurrentPrice(ctx, modelID, sourceID)
	if err != nil {
		r.logger.Warn().
			Int("model_id", modelID).
			Int("source_id", sourceID).
			Err(err).
			Msg("reconciler: LookupCurrentPrice failed; unchanged field will be 0")
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
		r.logger.Error().
			Str("field", string(field)).
			Int("model_id", modelID).
			Int("source_id", sourceID).
			Msg("reconciler: unknown PriceField; skipping publish to prevent data corruption")
		return
	}

	if err := r.store.PublishPrice(ctx, modelID, sourceID, input, output, confidence, underlyingProvider); err != nil {
		r.logger.Warn().
			Int("model_id", modelID).
			Int("source_id", sourceID).
			Err(err).
			Msg("reconciler: PublishPrice failed")
		return
	}

	// Resolve source name and provider for event payloads (shared by webhooks and Pub/Sub).
	// Skip the lookups entirely when no downstream consumer is active — both are DB
	// round-trips and they are only needed to populate fan-out payloads.
	if r.asynqClient == nil && r.redisClient == nil {
		return
	}
	sourceName, err := r.store.LookupSourceName(ctx, sourceID)
	if err != nil {
		r.logger.Warn().Int("source_id", sourceID).Err(err).Msg("reconciler: LookupSourceName failed; using empty string in event payload")
	}
	provider, err := r.store.LookupModelProvider(ctx, modelID)
	if err != nil {
		r.logger.Warn().Int("model_id", modelID).Err(err).Msg("reconciler: LookupModelProvider failed; using empty string in event payload")
	}

	event := webhooks.Payload{
		ModelID:        modelID,
		Provider:       provider,
		OldPriceInput:  currInput,
		OldPriceOutput: currOutput,
		NewPriceInput:  input,
		NewPriceOutput: output,
		ConfirmedAt:    time.Now().UTC(),
		Source:         sourceName,
	}

	if r.asynqClient != nil && len(activeWebhooks) > 0 {
		r.enqueueWebhooksWithEvent(ctx, event, activeWebhooks)
	}

	r.publishEvent(ctx, SSEEvent{Payload: event})
}

// enqueueWebhooksWithEvent fans out a webhook:deliver task to every active webhook registration.
// It accepts the pre-built webhooks.Payload to avoid redundant store lookups.
// activeWebhooks is pre-fetched once per reconciliation cycle by Reconcile() to
// avoid an N+1 query pattern. Errors are logged but never returned — a delivery
// failure must not abort reconciliation.
func (r *Reconciler) enqueueWebhooksWithEvent(
	ctx context.Context,
	event webhooks.Payload,
	activeWebhooks []WebhookRow,
) {
	for _, wh := range activeWebhooks {
		task, err := newWebhookDeliverTask(webhooks.TaskPayload{
			WebhookID: wh.ID,
			URL:       wh.URL,
			Secret:    wh.Secret,
			Event:     event,
		})
		if err != nil {
			r.logger.Warn().Str("webhook_id", wh.ID).Err(err).Msg("reconciler: failed to build webhook task")
			continue
		}
		if _, err := r.asynqClient.EnqueueContext(ctx, task); err != nil {
			r.logger.Warn().Str("webhook_id", wh.ID).Err(err).Msg("reconciler: failed to enqueue webhook delivery")
		}
	}
}

// newWebhookDeliverTask builds an asynq.Task for webhooks.TypeWebhookDeliver.
func newWebhookDeliverTask(p webhooks.TaskPayload) (*asynq.Task, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook task payload: %w", err)
	}
	return asynq.NewTask(webhooks.TypeWebhookDeliver,
		data,
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
	), nil
}
