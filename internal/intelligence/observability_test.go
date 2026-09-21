package intelligence

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"llm-pricing-api/internal/metrics"
)

// The helpers below gather the default registry rather than reading a metric
// child directly. Gathering is required to assert that a metric is *absent*:
// a plain Gauge or Counter child exists as soon as it is referenced, so a
// direct read reports 0 and cannot distinguish "never set" from "set to 0".
//
// prometheus/client_golang/prometheus/testutil is deliberately not used:
// testutil drags in github.com/kylelemons/godebug, which is not a go.mod
// requirement, and importing it would force an unrelated dependency change.

// recomputeFailures returns the current llm_capability_recompute_failures_total
// value and whether the family exists at all.
func recomputeFailures(t *testing.T) (float64, bool) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "llm_capability_recompute_failures_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			return metric.GetCounter().GetValue(), true
		}
	}
	return 0, false
}

// lastRecomputeTimestamp returns the current
// llm_capability_last_recompute_timestamp_seconds value and whether the family
// exists at all.
func lastRecomputeTimestamp(t *testing.T) (float64, bool) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "llm_capability_last_recompute_timestamp_seconds" {
			continue
		}
		for _, metric := range family.GetMetric() {
			return metric.GetGauge().GetValue(), true
		}
	}
	return 0, false
}

// recomputeDurationSamples returns the number of observations recorded in
// llm_capability_recompute_duration_seconds.
func recomputeDurationSamples(t *testing.T) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "llm_capability_recompute_duration_seconds" {
			continue
		}
		for _, metric := range family.GetMetric() {
			return metric.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

// TestObserveRecompute_Success covers the happy path: duration is observed and
// the last-success timestamp advances, while the failure counter does not move.
func TestObserveRecompute_Success(t *testing.T) {
	beforeFailures, _ := recomputeFailures(t)
	beforeSamples := recomputeDurationSamples(t)
	beforeCall := time.Now().Unix()

	observeRecompute(time.Now().Add(-1500*time.Millisecond), nil)

	afterFailures, _ := recomputeFailures(t)
	if afterFailures != beforeFailures {
		t.Errorf("failure counter delta = %v; want 0 on success", afterFailures-beforeFailures)
	}
	if got := recomputeDurationSamples(t); got != beforeSamples+1 {
		t.Errorf("duration samples delta = %d; want 1", got-beforeSamples)
	}
	timestamp, ok := lastRecomputeTimestamp(t)
	if !ok {
		t.Fatal("llm_capability_last_recompute_timestamp_seconds has no sample after a successful recompute")
	}
	if int64(timestamp) < beforeCall {
		t.Errorf("last recompute timestamp = %v; want >= %v", timestamp, beforeCall)
	}
}

// TestObserveRecompute_Failure covers the failure path: the failure counter
// increments, the duration is still observed (a slow failure is exactly what
// the dashboard must show), and the last-success timestamp does not advance.
func TestObserveRecompute_Failure(t *testing.T) {
	const sentinel = 1000.0
	metrics.CapabilityLastRecomputeTimestampSeconds.Set(sentinel)

	beforeFailures, _ := recomputeFailures(t)
	beforeSamples := recomputeDurationSamples(t)

	observeRecompute(time.Now().Add(-2*time.Second), errors.New("recompute failed"))

	afterFailures, ok := recomputeFailures(t)
	if !ok {
		t.Fatal("llm_capability_recompute_failures_total has no sample after a failure")
	}
	if afterFailures != beforeFailures+1 {
		t.Errorf("failure counter delta = %v; want 1", afterFailures-beforeFailures)
	}
	if got := recomputeDurationSamples(t); got != beforeSamples+1 {
		t.Errorf("duration samples delta = %d; want 1", got-beforeSamples)
	}
	if timestamp, _ := lastRecomputeTimestamp(t); timestamp != sentinel {
		t.Errorf("last recompute timestamp = %v; want unchanged %v after a failure", timestamp, sentinel)
	}
}
