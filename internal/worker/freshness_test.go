package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// mockFreshnessQuerier is a freshnessQuerier whose result the test controls.
type mockFreshnessQuerier struct {
	rows            []sourceFreshness
	err             error
	calls           int
	gotStaleAfter   time.Duration
	gotActiveWindow time.Duration
}

func (m *mockFreshnessQuerier) SourceFreshness(_ context.Context, staleAfter, activeWindow time.Duration) ([]sourceFreshness, error) {
	m.calls++
	m.gotStaleAfter = staleAfter
	m.gotActiveWindow = activeWindow
	return m.rows, m.err
}

// gaugeSamples gathers every sample of the named gauge family keyed by the
// value of the given label. Gathering the default registry (rather than a per-child read) is
// what lets a test assert a source is *absent* rather than merely zero: a
// GaugeVec child is created by the first WithLabelValues call, so reading an
// untouched label reports 0 and cannot distinguish "never set" from "set to 0".
//
// The task's alternative — prometheus/client_golang/prometheus/testutil — is
// deliberately not used: testutil drags in github.com/kylelemons/godebug, which
// is not currently a go.mod requirement, and importing it would force a
// dependency change unrelated to this feature.
func gaugeSamples(t *testing.T, name, label string) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	out := make(map[string]float64)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, m := range family.GetMetric() {
			key := ""
			for _, l := range m.GetLabel() {
				if l.GetName() == label {
					key = l.GetValue()
				}
			}
			out[key] = m.GetGauge().GetValue()
		}
	}
	return out
}

// gaugeValue returns the current value of a gauge sample for source, failing the
// test if the sample does not exist.
func gaugeValue(t *testing.T, name, source string) float64 {
	t.Helper()
	value, ok := gaugeSamples(t, name, "source")[source]
	if !ok {
		t.Fatalf("%s has no sample for source %q", name, source)
	}
	return value
}

// assertGaugeAbsent fails when the named gauge has a sample for source, which is
// how the "no rows must not emit a misleading zero" rule is verified.
func assertGaugeAbsent(t *testing.T, name, source string) {
	t.Helper()
	if _, ok := gaugeSamples(t, name, "source")[source]; ok {
		t.Errorf("%s must have no sample for source %q; got one", name, source)
	}
}

// TestFreshnessSampler_HealthyRows covers the all-fresh case: every published
// price was verified inside the staleness window, so the ratio is 0 and the
// timestamp/total gauges carry the source's real values.
func TestFreshnessSampler_HealthyRows(t *testing.T) {
	const source = "test_fresh_healthy"
	lastVerified := time.Unix(1_700_000_000, 0).UTC()

	q := &mockFreshnessQuerier{rows: []sourceFreshness{
		{Source: source, LastVerifiedAt: lastVerified, Prices: 4, Active: 4, Stale: 0},
	}}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := gaugeValue(t, "llm_source_last_success_timestamp_seconds", source); got != float64(lastVerified.Unix()) {
		t.Errorf("last success = %v; want %v", got, lastVerified.Unix())
	}
	if got := gaugeValue(t, "llm_prices_published", source); got != 4 {
		t.Errorf("prices total = %v; want 4", got)
	}
	if got := gaugeValue(t, "llm_prices_stale_ratio", source); got != 0 {
		t.Errorf("stale ratio = %v; want 0", got)
	}
}

// TestFreshnessSampler_AllRowsStale covers a source whose entire published set
// has fallen outside the freshness window: the ratio is 1, not 0 or 100.
func TestFreshnessSampler_AllRowsStale(t *testing.T) {
	const source = "test_fresh_all_stale"
	lastVerified := time.Unix(1_600_000_000, 0).UTC()

	q := &mockFreshnessQuerier{rows: []sourceFreshness{
		{Source: source, LastVerifiedAt: lastVerified, Prices: 3, Active: 3, Stale: 3},
	}}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := gaugeValue(t, "llm_prices_stale_ratio", source); got != 1 {
		t.Errorf("stale ratio = %v; want 1", got)
	}
	if got := gaugeValue(t, "llm_prices_published", source); got != 3 {
		t.Errorf("prices total = %v; want 3", got)
	}
}

// TestFreshnessSampler_SomeRowsStale covers the partial case the warning alert
// keys on: 1 of 4 prices stale must publish 0.25.
func TestFreshnessSampler_SomeRowsStale(t *testing.T) {
	const source = "test_fresh_some_stale"

	q := &mockFreshnessQuerier{rows: []sourceFreshness{
		{Source: source, LastVerifiedAt: time.Unix(1_700_000_500, 0).UTC(), Prices: 4, Active: 4, Stale: 1},
	}}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := gaugeValue(t, "llm_prices_stale_ratio", source); got != 0.25 {
		t.Errorf("stale ratio = %v; want 0.25", got)
	}
}

// TestFreshnessSampler_DelistedRowsExcludedFromRatio pins the #217 behaviour:
// rows outside the activity window — delisted upstream, or orphaned by a scraper
// change — leave the denominator, so the ratio measures the pipeline's health
// rather than accumulated history.
func TestFreshnessSampler_DelistedRowsExcludedFromRatio(t *testing.T) {
	const source = "test_fresh_delisted"
	lastVerified := time.Unix(1_700_000_000, 0).UTC()

	// 10 published rows, only 4 still active, 1 of those stale: the ratio is
	// 1/4 = 0.25, not 1/10 = 0.1.
	q := &mockFreshnessQuerier{rows: []sourceFreshness{
		{Source: source, LastVerifiedAt: lastVerified, Prices: 10, Active: 4, Stale: 1},
	}}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := gaugeValue(t, "llm_prices_stale_ratio", source); got != 0.25 {
		t.Errorf("stale ratio = %v; want 0.25 (active denominator)", got)
	}
	if got := gaugeValue(t, "llm_prices_active", source); got != 4 {
		t.Errorf("active = %v; want 4", got)
	}
	if got := gaugeValue(t, "llm_prices_published", source); got != 10 {
		t.Errorf("published = %v; want 10", got)
	}
}

// TestFreshnessSampler_NoActiveRowsSkipsRatio covers a source whose entire
// published set has aged out of the activity window: the timestamps are still
// published, but there is no denominator, so no ratio is emitted. A ratio here
// would divide by zero, and a zeroed one would read as "perfectly fresh".
func TestFreshnessSampler_NoActiveRowsSkipsRatio(t *testing.T) {
	const source = "test_fresh_no_active"
	lastVerified := time.Unix(1_600_000_000, 0).UTC()

	q := &mockFreshnessQuerier{rows: []sourceFreshness{
		{Source: source, LastVerifiedAt: lastVerified, Prices: 5, Active: 0, Stale: 0},
	}}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := gaugeValue(t, "llm_prices_published", source); got != 5 {
		t.Errorf("published = %v; want 5", got)
	}
	if got := gaugeValue(t, "llm_source_last_success_timestamp_seconds", source); got != float64(lastVerified.Unix()) {
		t.Errorf("last success = %v; want %v", got, lastVerified.Unix())
	}
	assertGaugeAbsent(t, "llm_prices_stale_ratio", source)
	assertGaugeAbsent(t, "llm_prices_active", source)
}

// TestFreshnessSampler_SourceWithNoRowsIsSkipped verifies that a source the
// query returns with a zero price count produces no gauge samples at all. A
// zeroed llm_prices_stale_ratio would read as "perfectly fresh", and a zeroed
// llm_source_last_success_timestamp_seconds as "verified in 1970"; either would
// mislead the dashboard and the alert rules.
func TestFreshnessSampler_SourceWithNoRowsIsSkipped(t *testing.T) {
	const emptySource = "test_fresh_no_rows"
	const realSource = "test_fresh_no_rows_real"

	q := &mockFreshnessQuerier{rows: []sourceFreshness{
		{Source: emptySource, Prices: 0, Active: 0, Stale: 0},
		{Source: realSource, LastVerifiedAt: time.Unix(1_700_000_100, 0).UTC(), Prices: 2, Active: 2, Stale: 0},
	}}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	for _, name := range []string{
		"llm_source_last_success_timestamp_seconds",
		"llm_prices_stale_ratio",
		"llm_prices_active",
		"llm_prices_published",
	} {
		assertGaugeAbsent(t, name, emptySource)
	}

	// The healthy sibling in the same sample is still published.
	if got := gaugeValue(t, "llm_prices_published", realSource); got != 2 {
		t.Errorf("sibling prices total = %v; want 2", got)
	}
}

// TestFreshnessSampler_EmptyResultSetsNothing verifies the genuine "no source
// has prices yet" case: a healthy empty result is not an error and emits
// nothing.
func TestFreshnessSampler_EmptyResultSetsNothing(t *testing.T) {
	q := &mockFreshnessQuerier{}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if q.calls != 1 {
		t.Fatalf("querier calls = %d; want 1", q.calls)
	}
}

// TestFreshnessSampler_QueryError verifies that a failed query is returned to
// the caller (so the ticker can log it) and that no gauges are touched.
func TestFreshnessSampler_QueryError(t *testing.T) {
	const source = "test_fresh_query_error"
	queryErr := errors.New("connection reset")

	q := &mockFreshnessQuerier{
		rows: []sourceFreshness{{Source: source, Prices: 1, Active: 1, Stale: 1}},
		err:  queryErr,
	}
	s := &FreshnessSampler{querier: q, staleAfter: DefaultStaleAfter, activeWindow: DefaultActiveWindow}

	err := s.Sample(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, queryErr) {
		t.Errorf("error chain should contain the query error; got: %v", err)
	}

	for _, name := range []string{
		"llm_source_last_success_timestamp_seconds",
		"llm_prices_stale_ratio",
		"llm_prices_active",
		"llm_prices_published",
	} {
		assertGaugeAbsent(t, name, source)
	}
}

// TestFreshnessSampler_PassesWindows verifies the configured thresholds are
// forwarded to the querier rather than hard-coded, since the SQL binds both as
// parameters.
func TestFreshnessSampler_PassesWindows(t *testing.T) {
	const staleAfter = 6 * time.Hour
	q := &mockFreshnessQuerier{}
	s := &FreshnessSampler{querier: q, staleAfter: staleAfter, activeWindow: DefaultActiveWindow}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if q.gotStaleAfter != staleAfter {
		t.Errorf("querier staleAfter = %v; want %v", q.gotStaleAfter, staleAfter)
	}
	if q.gotActiveWindow != DefaultActiveWindow {
		t.Errorf("querier activeWindow = %v; want %v", q.gotActiveWindow, DefaultActiveWindow)
	}
}

// TestDefaultStaleAfterMatchesProductPromise pins the threshold to the 24-hour
// freshness promise and to the medium-confidence window in
// internal/api.ComputeTrustMeta. If one moves, the other must move with it.
func TestDefaultStaleAfterMatchesProductPromise(t *testing.T) {
	if DefaultStaleAfter != 24*time.Hour {
		t.Errorf("DefaultStaleAfter = %v; want 24h", DefaultStaleAfter)
	}
}

// TestDefaultActiveWindow pins the activity window to comfortably more than the
// scrape cadence: every price source runs at least daily, so a week of silence
// means a model is gone (delisted, renamed, or orphaned) rather than merely
// slow. Too short a window would drop models between scrapes; too long a one
// would let delisted rows inflate the ratio again (#217).
func TestDefaultActiveWindow(t *testing.T) {
	if DefaultActiveWindow != 7*24*time.Hour {
		t.Errorf("DefaultActiveWindow = %v; want 168h", DefaultActiveWindow)
	}
}
