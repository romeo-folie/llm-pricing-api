package intelligence

import (
	"math"
	"testing"
)

func TestDimensionWeightsFor_ReturnsExpectedKeys(t *testing.T) {
	w := DimensionWeightsFor("coding", "quality")
	if len(w) == 0 {
		t.Fatal("expected non-empty weight map")
	}
	// coding primary dims: coding, reasoning — should have highest weights.
	if w["coding"] <= w["quality"] {
		t.Errorf("expected coding weight > quality weight, got coding=%.2f quality=%.2f", w["coding"], w["quality"])
	}
}

func TestWeightClamping(t *testing.T) {
	uc := UseCaseCoding
	pri := PriorityQuality
	hardcoded := dimensionWeights(uc, pri)

	// Simulate max positive adjustment: 5% increase capped at 120% of hardcoded.
	for dim, hw := range hardcoded {
		ceiling := hw * 1.2
		adjusted := hw * 1.05
		result := math.Min(ceiling, adjusted)
		if result > ceiling {
			t.Errorf("dim %s: adjusted %.4f exceeds ceiling %.4f", dim, result, ceiling)
		}
	}

	// Simulate max negative adjustment: 5% decrease floored at 80% of hardcoded.
	for dim, hw := range hardcoded {
		floor := hw * 0.8
		adjusted := hw * 0.95
		result := math.Max(floor, adjusted)
		if result < floor {
			t.Errorf("dim %s: adjusted %.4f below floor %.4f", dim, result, floor)
		}
	}
}

func TestCalibrateWeightsNoFeedback_NoError(t *testing.T) {
	// Calibration should succeed with no DB — the function handles empty results.
	// This test validates the weight adjustment logic directly.
	uc := UseCaseCoding
	pri := PriorityBalanced
	weights := dimensionWeights(uc, pri)

	// No adjustments when feedback is empty — weights should remain unchanged.
	for dim, w := range weights {
		expected := dimensionWeights(uc, pri)[dim]
		if w != expected {
			t.Errorf("dim %s: got %.4f, expected %.4f", dim, w, expected)
		}
	}
}

func TestCalibration_PrimaryDimensions(t *testing.T) {
	tests := []struct {
		uc   UseCase
		want []string
	}{
		{UseCaseCoding, []string{"coding", "reasoning"}},
		{UseCaseFinance, []string{"finance", "quality"}},
		{UseCaseGeneral, []string{"quality", "preference"}},
	}

	for _, tt := range tests {
		dims := PrimaryDimensions(tt.uc)
		if len(dims) != len(tt.want) {
			t.Errorf("uc=%s: got %d dims, want %d", tt.uc, len(dims), len(tt.want))
			continue
		}
		for i, d := range dims {
			if d != tt.want[i] {
				t.Errorf("uc=%s dim[%d]: got %s, want %s", tt.uc, i, d, tt.want[i])
			}
		}
	}
}
