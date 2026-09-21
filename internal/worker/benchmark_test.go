package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"llm-pricing-api/internal/intelligence"
	"llm-pricing-api/internal/metrics"
)

// mockBenchmarkEvidenceQuerier is a benchmarkEvidenceQuerier whose result the
// test controls.
type mockBenchmarkEvidenceQuerier struct {
	rows          []benchmarkEvidence
	err           error
	calls         int
	gotStaleAfter time.Duration
}

func (m *mockBenchmarkEvidenceQuerier) BenchmarkEvidenceFreshness(_ context.Context, staleAfter time.Duration) ([]benchmarkEvidence, error) {
	m.calls++
	m.gotStaleAfter = staleAfter
	return m.rows, m.err
}

// benchmarkGaugeSamples gathers every sample of the named gauge family keyed by
// its benchmark label. Gathering the default registry (rather than a per-child
// read) is what lets a test assert a benchmark is *absent* rather than merely
// zero: a GaugeVec child is created by the first WithLabelValues call, so
// reading an untouched label reports 0 and cannot distinguish "never set" from
// "set to 0".
//
// prometheus/client_golang/prometheus/testutil is deliberately not used:
// testutil drags in github.com/kylelemons/godebug, which is not a go.mod
// requirement, and importing it would force an unrelated dependency change.
func benchmarkGaugeSamples(t *testing.T, name string) map[string]float64 {
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
		for _, metric := range family.GetMetric() {
			benchmark := ""
			for _, label := range metric.GetLabel() {
				if label.GetName() == "benchmark" {
					benchmark = label.GetValue()
				}
			}
			out[benchmark] = metric.GetGauge().GetValue()
		}
	}
	return out
}

// benchmarkGaugeValue returns the current value of a gauge sample for
// benchmarkName, failing the test if the sample does not exist.
func benchmarkGaugeValue(t *testing.T, name, benchmarkName string) float64 {
	t.Helper()
	value, ok := benchmarkGaugeSamples(t, name)[benchmarkName]
	if !ok {
		t.Fatalf("%s has no sample for benchmark %q", name, benchmarkName)
	}
	return value
}

// assertBenchmarkGaugeAbsent fails when the named gauge has a sample for
// benchmarkName, which is how the "no rows must not emit a misleading zero"
// rule is verified.
func assertBenchmarkGaugeAbsent(t *testing.T, name, benchmarkName string) {
	t.Helper()
	if _, ok := benchmarkGaugeSamples(t, name)[benchmarkName]; ok {
		t.Errorf("%s must have no sample for benchmark %q; got one", name, benchmarkName)
	}
}

// TestBenchmarkSampler_HealthyRows covers the all-fresh case: every active
// evidence row was evaluated inside the staleness window, so the ratio is 0 and
// the count gauge carries the real evidence total.
func TestBenchmarkSampler_HealthyRows(t *testing.T) {
	const benchmark = "test_bench_healthy"

	q := &mockBenchmarkEvidenceQuerier{rows: []benchmarkEvidence{
		{Benchmark: benchmark, Active: 4, Stale: 0},
	}}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_active", benchmark); got != 4 {
		t.Errorf("active evidence = %v; want 4", got)
	}
	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_stale_ratio", benchmark); got != 0 {
		t.Errorf("stale ratio = %v; want 0", got)
	}
}

// TestBenchmarkSampler_AllRowsStale covers the SWE-bench production shape: the
// newest published evaluation is older than the threshold, so the ratio is 1
// even though the source is re-scraped daily.
func TestBenchmarkSampler_AllRowsStale(t *testing.T) {
	const benchmark = "test_bench_all_stale"

	q := &mockBenchmarkEvidenceQuerier{rows: []benchmarkEvidence{
		{Benchmark: benchmark, Active: 3, Stale: 3},
	}}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_stale_ratio", benchmark); got != 1 {
		t.Errorf("stale ratio = %v; want 1", got)
	}
	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_active", benchmark); got != 3 {
		t.Errorf("active evidence = %v; want 3", got)
	}
}

// TestBenchmarkSampler_SomeRowsStale covers the partial case the warning alert
// keys on: 1 of 4 active rows stale must publish 0.25.
func TestBenchmarkSampler_SomeRowsStale(t *testing.T) {
	const benchmark = "test_bench_some_stale"

	q := &mockBenchmarkEvidenceQuerier{rows: []benchmarkEvidence{
		{Benchmark: benchmark, Active: 4, Stale: 1},
	}}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_stale_ratio", benchmark); got != 0.25 {
		t.Errorf("stale ratio = %v; want 0.25", got)
	}
}

// TestBenchmarkSampler_BenchmarkWithNoRowsIsSkipped verifies that a benchmark
// the query returns with a zero active count produces no gauge samples at all,
// while a healthy sibling in the same result is still published. A zeroed
// llm_benchmark_evidence_stale_ratio would read as "perfectly fresh".
func TestBenchmarkSampler_BenchmarkWithNoRowsIsSkipped(t *testing.T) {
	const emptyBenchmark = "test_bench_no_rows"
	const realBenchmark = "test_bench_no_rows_real"

	q := &mockBenchmarkEvidenceQuerier{rows: []benchmarkEvidence{
		{Benchmark: emptyBenchmark, Active: 0, Stale: 0},
		{Benchmark: realBenchmark, Active: 2, Stale: 0},
	}}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	for _, name := range []string{
		"llm_benchmark_evidence_active",
		"llm_benchmark_evidence_stale_ratio",
	} {
		assertBenchmarkGaugeAbsent(t, name, emptyBenchmark)
	}

	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_active", realBenchmark); got != 2 {
		t.Errorf("sibling active evidence = %v; want 2", got)
	}
}

// TestBenchmarkSampler_AbsentBenchmarkIsNotZeroed verifies that a benchmark
// with no active evidence — for example one that has never been ingested —
// keeps its previous sample instead of being reset to zero. Zeroing it would
// erase the only visible trace of a benchmark that stopped receiving evidence.
func TestBenchmarkSampler_AbsentBenchmarkIsNotZeroed(t *testing.T) {
	const absentBenchmark = "test_bench_absent_untouched"
	const sampledBenchmark = "test_bench_absent_sampled"

	metrics.BenchmarkEvidenceActive.WithLabelValues(absentBenchmark).Set(7)
	metrics.BenchmarkEvidenceStaleRatio.WithLabelValues(absentBenchmark).Set(0.9)

	q := &mockBenchmarkEvidenceQuerier{rows: []benchmarkEvidence{
		{Benchmark: sampledBenchmark, Active: 1, Stale: 0},
	}}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_active", absentBenchmark); got != 7 {
		t.Errorf("absent benchmark active evidence = %v; want preserved 7", got)
	}
	if got := benchmarkGaugeValue(t, "llm_benchmark_evidence_stale_ratio", absentBenchmark); got != 0.9 {
		t.Errorf("absent benchmark stale ratio = %v; want preserved 0.9", got)
	}
}

// TestBenchmarkSampler_QueryError verifies that a failed query is returned to
// the caller (so the ticker can log it) and that no gauges are touched.
func TestBenchmarkSampler_QueryError(t *testing.T) {
	const benchmark = "test_bench_query_error"
	queryErr := errors.New("connection reset")

	q := &mockBenchmarkEvidenceQuerier{
		rows: []benchmarkEvidence{{Benchmark: benchmark, Active: 1, Stale: 1}},
		err:  queryErr,
	}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	err := s.Sample(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, queryErr) {
		t.Errorf("error chain should contain the query error; got: %v", err)
	}

	for _, name := range []string{
		"llm_benchmark_evidence_active",
		"llm_benchmark_evidence_stale_ratio",
	} {
		assertBenchmarkGaugeAbsent(t, name, benchmark)
	}
}

// TestBenchmarkSampler_EmptyResult verifies the genuine "no benchmark has
// evidence yet" case: a healthy empty result is not an error and emits nothing.
func TestBenchmarkSampler_EmptyResult(t *testing.T) {
	q := &mockBenchmarkEvidenceQuerier{}
	s := &BenchmarkSampler{querier: q, staleAfter: DefaultBenchmarkStaleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if q.calls != 1 {
		t.Fatalf("querier calls = %d; want 1", q.calls)
	}
}

// TestBenchmarkSampler_PassesStaleAfter verifies the configured threshold is
// forwarded to the querier rather than hard-coded, since the SQL binds it as a
// parameter.
func TestBenchmarkSampler_PassesStaleAfter(t *testing.T) {
	const staleAfter = 30 * 24 * time.Hour
	q := &mockBenchmarkEvidenceQuerier{}
	s := &BenchmarkSampler{querier: q, staleAfter: staleAfter}

	if err := s.Sample(context.Background()); err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if q.gotStaleAfter != staleAfter {
		t.Errorf("querier staleAfter = %v; want %v", q.gotStaleAfter, staleAfter)
	}
}

// TestDefaultBenchmarkStaleAfterMatchesScorer pins the sampler threshold to the
// capability scorer's staleness rule. If one moves, the other must move with
// it, or the gauge will disagree with the freshness label the API serves.
func TestDefaultBenchmarkStaleAfterMatchesScorer(t *testing.T) {
	if DefaultBenchmarkStaleAfter != 90*24*time.Hour {
		t.Errorf("DefaultBenchmarkStaleAfter = %v; want 90 days", DefaultBenchmarkStaleAfter)
	}
	if DefaultBenchmarkStaleAfter != time.Duration(intelligence.StalenessThresholdDays)*24*time.Hour {
		t.Errorf("DefaultBenchmarkStaleAfter = %v; want intelligence.StalenessThresholdDays (%d) days",
			DefaultBenchmarkStaleAfter, intelligence.StalenessThresholdDays)
	}
}
