package swebench

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"llm-pricing-api/internal/scraper/slugmap"
)

// slugResolutionCounts gathers llm_slug_resolutions_total keyed by its result
// label. Gathering the default registry (rather than reading a specific child)
// is what lets a test compare before/after values: a CounterVec child exists
// only after its first WithLabelValues call, so a per-child read cannot
// distinguish "never incremented" from "incremented then reset".
//
// prometheus/client_golang/prometheus/testutil is deliberately not used:
// testutil drags in github.com/kylelemons/godebug, which is not a go.mod
// requirement, and importing it would force an unrelated dependency change.
func slugResolutionCounts(t *testing.T) map[string]float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default registry: %v", err)
	}
	out := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "llm_slug_resolutions_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			result := ""
			for _, label := range metric.GetLabel() {
				if label.GetName() == "result" {
					result = label.GetValue()
				}
			}
			out[result] = metric.GetCounter().GetValue()
		}
	}
	return out
}

// TestResolveSlug_RecordsOutcome verifies the SWE-bench entry-level resolution
// counter distinguishes all three slugmap outcomes, so the benchmark audit can
// tell "upstream publishes few mappable models" from "the resolver rejects most
// entries".
func TestResolveSlug_RecordsOutcome(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSlug    string
		wantOutcome slugmap.Outcome
	}{
		{"resolved", "gpt-4o", "openai/gpt-4o", slugmap.OutcomeResolved},
		{"unknown", "not-a-real-model-xyz", "", slugmap.OutcomeUnknown},
		{"ambiguous", "gpt-4o-mini-20250101", "openai/gpt-4o-mini", slugmap.OutcomeAmbiguous},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := slugResolutionCounts(t)

			slug, outcome := resolveSlug(tc.input)
			if slug != tc.wantSlug || outcome != tc.wantOutcome {
				t.Fatalf("resolveSlug(%q) = (%q, %q); want (%q, %q)",
					tc.input, slug, outcome, tc.wantSlug, tc.wantOutcome)
			}

			after := slugResolutionCounts(t)
			if got := after[string(tc.wantOutcome)] - before[string(tc.wantOutcome)]; got != 1 {
				t.Errorf("llm_slug_resolutions_total{result=%q} delta = %v; want 1", tc.wantOutcome, got)
			}
			for _, other := range []slugmap.Outcome{
				slugmap.OutcomeResolved, slugmap.OutcomeUnknown, slugmap.OutcomeAmbiguous,
			} {
				if other == tc.wantOutcome {
					continue
				}
				if got := after[string(other)] - before[string(other)]; got != 0 {
					t.Errorf("llm_slug_resolutions_total{result=%q} moved by %v; want 0", other, got)
				}
			}
		})
	}
}
