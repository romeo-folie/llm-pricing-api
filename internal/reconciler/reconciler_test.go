package reconciler_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"llm-pricing-api/internal/diff"
	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/reconciler"
)

// ----- mockStore -----

type mockStore struct {
	mu sync.Mutex // guards published and flagged

	sourceIDs     map[string]int
	modelIDs      map[string]int
	currentPrices map[[2]int]struct{ input, output float64 } // keyed by [modelID, sourceID]

	published []publishCall
	flagged   []flagCall

	publishErr         error
	flagErr            error
	lookupCurrentPrErr error // if set, LookupCurrentPrice returns this error
}

type publishCall struct {
	modelID    int
	sourceID   int
	input      float64
	output     float64
	confidence models.Confidence
}

type flagCall struct {
	modelID   int
	sourceAID int
	sourceBID int
	field     models.PriceField
	valueA    float64
	valueB    float64
	deltaPct  float64
}

func (m *mockStore) LookupSourceID(_ context.Context, name string) (int, error) {
	if id, ok := m.sourceIDs[name]; ok {
		return id, nil
	}
	return 0, errors.New("source not found: " + name)
}

func (m *mockStore) LookupModelID(_ context.Context, slug string) (int, error) {
	if id, ok := m.modelIDs[slug]; ok {
		return id, nil
	}
	return 0, errors.New("model not found: " + slug)
}

func (m *mockStore) LookupCurrentPrice(_ context.Context, modelID, sourceID int) (float64, float64, bool, error) {
	if m.lookupCurrentPrErr != nil {
		return 0, 0, false, m.lookupCurrentPrErr
	}
	if p, ok := m.currentPrices[[2]int{modelID, sourceID}]; ok {
		return p.input, p.output, true, nil
	}
	return 0, 0, false, nil
}

func (m *mockStore) PublishPrice(_ context.Context, modelID, sourceID int, input, output float64, confidence models.Confidence) error {
	if m.publishErr != nil {
		return m.publishErr
	}
	m.mu.Lock()
	m.published = append(m.published, publishCall{modelID, sourceID, input, output, confidence})
	m.mu.Unlock()
	return nil
}

func (m *mockStore) FlagDiscrepancy(_ context.Context, modelID, sourceAID, sourceBID int, field models.PriceField, valueA, valueB float64, deltaPct float64) error {
	if m.flagErr != nil {
		return m.flagErr
	}
	m.mu.Lock()
	m.flagged = append(m.flagged, flagCall{modelID, sourceAID, sourceBID, field, valueA, valueB, deltaPct})
	m.mu.Unlock()
	return nil
}

func newMockStore() *mockStore {
	return &mockStore{
		sourceIDs:     map[string]int{"openrouter": 1, "litellm": 2, "openai-docs": 3},
		modelIDs:      map[string]int{"openai/gpt-4o": 10, "anthropic/claude-3-5-sonnet": 20},
		currentPrices: map[[2]int]struct{ input, output float64 }{},
	}
}

// ----- helpers -----

func inputDiff(slug, source string, newVal float64) diff.PriceDiff {
	return diff.PriceDiff{
		ModelSlug: slug, Field: models.PriceFieldInput,
		OldValue: 0, NewValue: newVal, Source: source,
	}
}

func outputDiff(slug, source string, newVal float64) diff.PriceDiff {
	return diff.PriceDiff{
		ModelSlug: slug, Field: models.PriceFieldOutput,
		OldValue: 0, NewValue: newVal, Source: source,
	}
}

// ----- tests -----

func TestReconcile_Empty_NoDBCalls(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("expected no error for empty diffs, got %v", err)
	}
	if len(s.published) != 0 || len(s.flagged) != 0 {
		t.Error("empty diffs should produce no DB calls")
	}
}

func TestReconcile_TwoSourceAgreement_PublishesWithHighConfidence(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
	}

	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.published) == 0 {
		t.Fatal("expected a publish call on 2-source agreement")
	}
	got := s.published[0]
	if got.modelID != 10 {
		t.Errorf("expected modelID=10, got %d", got.modelID)
	}
	if got.input != 0.000005 {
		t.Errorf("expected input=0.000005, got %g", got.input)
	}
	if got.confidence != models.ConfidenceHigh {
		t.Errorf("expected high confidence on 2-source agreement, got %s", got.confidence)
	}
}

func TestReconcile_TwoSourceAgreement_DoesNotFlag(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
	}

	_ = r.Reconcile(context.Background(), diffs)
	if len(s.flagged) != 0 {
		t.Error("2-source agreement should not produce a discrepancy flag")
	}
}

func TestReconcile_TwoSourceDisagreement_FlagsReview(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	// 100% disagreement between sources
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000010),
	}

	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.flagged) == 0 {
		t.Fatal("expected a discrepancy flag for >5% source disagreement")
	}
	f := s.flagged[0]
	if f.modelID != 10 {
		t.Errorf("expected modelID=10, got %d", f.modelID)
	}
	if f.field != models.PriceFieldInput {
		t.Errorf("expected field=input, got %s", f.field)
	}
	if f.deltaPct < 0.05 {
		t.Errorf("expected deltaPct > 5%%, got %g", f.deltaPct)
	}
}

func TestReconcile_TwoSourceDisagreement_DoesNotPublish(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000010),
	}

	_ = r.Reconcile(context.Background(), diffs)
	if len(s.published) != 0 {
		t.Error("discrepancy should not trigger a price publish")
	}
}

func TestReconcile_MinorDisagreement_Publishes(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	// 2% difference — below the 5% threshold, should publish (not flag)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005000),
		inputDiff("openai/gpt-4o", "litellm", 0.000005100), // ~2% higher
	}

	_ = r.Reconcile(context.Background(), diffs)
	if len(s.published) == 0 {
		t.Error("minor disagreement (<5%) should publish, not flag")
	}
	if len(s.flagged) != 0 {
		t.Error("minor disagreement (<5%) should not produce a flag")
	}
}

func TestReconcile_SingleSource_FirstCycle_DoesNotPublish(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
	}

	_ = r.Reconcile(context.Background(), diffs)
	if len(s.published) != 0 {
		t.Error("single-source first cycle should hold in pending, not publish")
	}
}

func TestReconcile_SingleSource_SecondCycle_PublishesMediumConfidence(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	d := inputDiff("openai/gpt-4o", "openrouter", 0.000005)

	// Cycle 1: goes to pending
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{d})
	if len(s.published) != 0 {
		t.Fatal("cycle 1: should not publish")
	}

	// Cycle 2: same value → auto-publish
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{d})
	if len(s.published) == 0 {
		t.Fatal("cycle 2: same value should trigger auto-publish")
	}
	if s.published[0].confidence != models.ConfidenceMedium {
		t.Errorf("single-source auto-publish should use medium confidence, got %s", s.published[0].confidence)
	}
}

func TestReconcile_SingleSource_ValueChanges_ResetsCounter(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)

	// Cycle 1: value A
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
	})

	// Cycle 2: value B (changed) — counter resets, should NOT publish
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000006),
	})

	if len(s.published) != 0 {
		t.Error("value change between cycles should reset counter; no publish expected")
	}
}

func TestReconcile_SingleSource_ThirdCycleAfterChange_Publishes(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)

	// Cycle 1: value A
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{inputDiff("openai/gpt-4o", "openrouter", 0.000005)})
	// Cycle 2: value B (changed) — resets
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{inputDiff("openai/gpt-4o", "openrouter", 0.000006)})
	// Cycle 3: value B again → now fetchCount == 2 → publish
	_ = r.Reconcile(context.Background(), []diff.PriceDiff{inputDiff("openai/gpt-4o", "openrouter", 0.000006)})

	if len(s.published) == 0 {
		t.Error("third cycle with same changed value should publish (fetchCount==2 for new value)")
	}
}

func TestReconcile_UnknownModel_SkipsGracefully(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("unknown/no-such-model", "openrouter", 0.000005),
		inputDiff("unknown/no-such-model", "litellm", 0.000005),
	}

	err := r.Reconcile(context.Background(), diffs)
	if err != nil {
		t.Fatalf("unknown model should be skipped (no error), got %v", err)
	}
	if len(s.published) != 0 {
		t.Error("unknown model: expected no publish")
	}
}

func TestReconcile_UnknownSource_SkipsGracefully(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	// One known source, one unknown
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "unknown-source", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
	}

	err := r.Reconcile(context.Background(), diffs)
	if err != nil {
		t.Fatalf("unknown source should be skipped (no error), got %v", err)
	}
	// Only one valid source remains → single-source → goes to pending (not published)
	if len(s.published) != 0 {
		t.Error("with 1 valid source after filtering unknown: should go to pending, not publish")
	}
}

func TestReconcile_RepeatedDiscrepancy_DoesNotReturnError(t *testing.T) {
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000010),
	}

	// Both calls should succeed; DB-level dedup handles idempotency via ON CONFLICT DO NOTHING
	err1 := r.Reconcile(context.Background(), diffs)
	err2 := r.Reconcile(context.Background(), diffs)
	if err1 != nil || err2 != nil {
		t.Errorf("repeated discrepancy flags should not propagate errors: %v, %v", err1, err2)
	}
}

func TestReconcile_FlagDiscrepancyError_DoesNotPropagateToReconcile(t *testing.T) {
	s := newMockStore()
	s.flagErr = errors.New("simulated DB constraint violation")
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000010),
	}

	// flagErr is set but Reconcile should not propagate it
	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Errorf("FlagDiscrepancy error should be logged, not returned from Reconcile: %v", err)
	}
}

func TestReconcile_LookupCurrentPriceError_StillPublishesWithZeroForUnchangedField(t *testing.T) {
	s := newMockStore()
	s.lookupCurrentPrErr = errors.New("simulated DB read error")
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
	}

	// LookupCurrentPrice fails, but publish should still proceed using 0 for the other field.
	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Fatalf("LookupCurrentPrice error should not propagate from Reconcile: %v", err)
	}
	if len(s.published) == 0 {
		t.Error("publish should proceed even when LookupCurrentPrice fails (output defaults to 0)")
	}
}

func TestReconcile_PublishPriceError_DoesNotPropagateToReconcile(t *testing.T) {
	s := newMockStore()
	s.publishErr = errors.New("simulated DB write failure")
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
	}

	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Errorf("PublishPrice error should be logged, not returned from Reconcile: %v", err)
	}
	// publishErr was set, so nothing was appended
	if len(s.published) != 0 {
		t.Error("expected no successful publish when store returns an error")
	}
}

func TestReconcile_BothSourcesZeroPrice_DoesNotPanic(t *testing.T) {
	// Both sources report 0: allAgree is true (0==0 within epsilon) so the
	// allAgree fast-path fires and publishes with (0, 0). Scrapers already
	// filter out 0-priced models so this is a defence-in-depth test; the
	// important invariant is that it does not panic or return an error.
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		{ModelSlug: "openai/gpt-4o", Field: models.PriceFieldInput, NewValue: 0, Source: "litellm"},
		{ModelSlug: "openai/gpt-4o", Field: models.PriceFieldInput, NewValue: 0, Source: "openrouter"},
	}

	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both agreed at 0 → publishes (reconciler trusts scrapers to filter zeros upstream)
	if len(s.published) == 0 {
		t.Error("expected publish for zero-price 2-source agreement (scraper is responsible for filtering)")
	}
	if len(s.flagged) != 0 {
		t.Error("expected no flag when both sources agree")
	}
}

func TestReconcile_ThreeSource_TwoAgreeOneOutlier_FlagsAndPublishes(t *testing.T) {
	// litellm + openrouter both report 0.000005 (exact agreement).
	// openai-docs reports 0.000005350, which is ~6.5% higher → outlier >5%.
	// Expected: flag the discrepancy AND still publish the 2-source consensus.
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
		inputDiff("openai/gpt-4o", "openai-docs", 0.000005350), // ~6.5% higher
		inputDiff("openai/gpt-4o", "openrouter", 0.000005),
	}

	if err := r.Reconcile(context.Background(), diffs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.flagged) == 0 {
		t.Fatal("expected discrepancy flag for outlier source (openai-docs)")
	}
	if len(s.published) == 0 {
		t.Fatal("expected publish: 2-source consensus (litellm+openrouter) should not be blocked by outlier")
	}
	pub := s.published[0]
	if pub.input != 0.000005 {
		t.Errorf("expected consensus value 0.000005, got %g", pub.input)
	}
	if pub.confidence != models.ConfidenceHigh {
		t.Errorf("expected ConfidenceHigh for 2-source exact consensus, got %s", pub.confidence)
	}
}

func TestReconcile_ThreeSource_AllDisagree_FlagsNoPublish(t *testing.T) {
	// All three sources disagree with each other by >5% — no 2-source consensus.
	// Expected: flag, no publish.
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		inputDiff("openai/gpt-4o", "litellm", 0.000005),
		inputDiff("openai/gpt-4o", "openai-docs", 0.000008), // ~60% higher
		inputDiff("openai/gpt-4o", "openrouter", 0.000012),  // >5% from both others
	}

	_ = r.Reconcile(context.Background(), diffs)
	if len(s.flagged) == 0 {
		t.Fatal("expected discrepancy flag when all sources disagree")
	}
	if len(s.published) != 0 {
		t.Error("expected no publish when there is no 2-source consensus")
	}
}

func TestReconcile_ConcurrentAccess_NoRace(t *testing.T) {
	// Verify that Reconciler is safe for concurrent use.
	// This test is most meaningful when run with -race.
	s := newMockStore()
	r := reconciler.NewWithStore(s)
	d := inputDiff("openai/gpt-4o", "openrouter", 0.000005)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Reconcile(context.Background(), []diff.PriceDiff{d})
		}()
	}
	wg.Wait()
}

func TestReconcile_OutputField_PublishesCorrectField(t *testing.T) {
	s := newMockStore()
	// Set a current input price so LookupCurrentPrice returns something meaningful
	// After alphabetical sort litellm (ID=2) is first, so publish uses sourceID=2.
	s.currentPrices[[2]int{10, 2}] = struct{ input, output float64 }{0.000005, 0}
	r := reconciler.NewWithStore(s)
	diffs := []diff.PriceDiff{
		outputDiff("openai/gpt-4o", "openrouter", 0.000020),
		outputDiff("openai/gpt-4o", "litellm", 0.000020),
	}

	_ = r.Reconcile(context.Background(), diffs)
	if len(s.published) == 0 {
		t.Fatal("expected publish on 2-source output agreement")
	}
	// output should be the new value; input should come from LookupCurrentPrice
	pub := s.published[0]
	if pub.output != 0.000020 {
		t.Errorf("expected output=0.000020, got %g", pub.output)
	}
	if pub.input != 0.000005 {
		t.Errorf("expected input=0.000005 (from current prices), got %g", pub.input)
	}
}
