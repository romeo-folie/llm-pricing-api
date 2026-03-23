package intelligence

import (
	"testing"
)


// makeCapScores creates CapabilityScore entries for the given dimension→score pairs.
func makeCapScores(modelID int, scores map[string]float64) []CapabilityScore {
	out := make([]CapabilityScore, 0, len(scores))
	for dim, s := range scores {
		v := s
		out = append(out, CapabilityScore{
			ModelID:        modelID,
			Dimension:      dim,
			Score:          &v,
			Confidence:     "high",
			BenchmarkCount: 2,
			Freshness:      "fresh",
		})
	}
	return out
}

func TestScoreModel_EmptyScores(t *testing.T) {
	score := ScoreModel(1, UseCaseCoding, PriorityQuality, nil)
	if score != 0 {
		t.Errorf("expected 0 for empty scores, got %f", score)
	}
	score = ScoreModel(1, UseCaseGeneral, PriorityBalanced, []CapabilityScore{})
	if score != 0 {
		t.Errorf("expected 0 for empty slice, got %f", score)
	}
}

func TestScoreModel_CodingQuality(t *testing.T) {
	// Model A: strong coding+reasoning. Model B: weak coding, strong writing.
	scoresA := makeCapScores(1, map[string]float64{
		"coding":    90,
		"reasoning": 85,
		"quality":   80,
		"writing":   50,
	})
	scoresB := makeCapScores(2, map[string]float64{
		"coding":    40,
		"reasoning": 45,
		"quality":   70,
		"writing":   95,
	})

	scoreA := ScoreModel(1, UseCaseCoding, PriorityQuality, scoresA)
	scoreB := ScoreModel(2, UseCaseCoding, PriorityQuality, scoresB)

	if scoreA <= scoreB {
		t.Errorf("coding+quality: model A (%f) should rank above model B (%f)", scoreA, scoreB)
	}
}

func TestScoreModel_FinanceCost(t *testing.T) {
	// Model A: strong finance. Model B: moderate finance.
	scoresA := makeCapScores(1, map[string]float64{
		"finance": 88,
		"quality": 82,
	})
	scoresB := makeCapScores(2, map[string]float64{
		"finance": 55,
		"quality": 90,
	})

	scoreA := ScoreModel(1, UseCaseFinance, PriorityCost, scoresA)
	scoreB := ScoreModel(2, UseCaseFinance, PriorityCost, scoresB)

	if scoreA <= scoreB {
		t.Errorf("finance+cost: model A (%f) should rank above model B (%f)", scoreA, scoreB)
	}
}

func TestScoreModel_AgenticBalanced(t *testing.T) {
	// Model A: strong agentic+tool_use+reasoning. Model B: strong elsewhere.
	scoresA := makeCapScores(1, map[string]float64{
		"agentic":   92,
		"tool_use":  88,
		"reasoning": 85,
		"writing":   60,
	})
	scoresB := makeCapScores(2, map[string]float64{
		"agentic":   50,
		"tool_use":  45,
		"reasoning": 60,
		"writing":   95,
	})

	scoreA := ScoreModel(1, UseCaseAgentic, PriorityBalanced, scoresA)
	scoreB := ScoreModel(2, UseCaseAgentic, PriorityBalanced, scoresB)

	if scoreA <= scoreB {
		t.Errorf("agentic+balanced: model A (%f) should rank above model B (%f)", scoreA, scoreB)
	}
}

func TestScoreModel_GeneralSpeed(t *testing.T) {
	// Model A: strong quality+preference. Model B: moderate quality+preference.
	scoresA := makeCapScores(1, map[string]float64{
		"quality":    90,
		"preference": 88,
	})
	scoresB := makeCapScores(2, map[string]float64{
		"quality":    60,
		"preference": 55,
	})

	scoreA := ScoreModel(1, UseCaseGeneral, PrioritySpeed, scoresA)
	scoreB := ScoreModel(2, UseCaseGeneral, PrioritySpeed, scoresB)

	if scoreA <= scoreB {
		t.Errorf("general+speed: model A (%f) should rank above model B (%f)", scoreA, scoreB)
	}
}

func TestScoreModel_ClampedOutput(t *testing.T) {
	// Even with extreme values, output should be clamped to 0–100.
	scores := makeCapScores(1, map[string]float64{
		"quality": 150,
	})
	score := ScoreModel(1, UseCaseGeneral, PriorityBalanced, scores)
	if score > 100 {
		t.Errorf("score should be clamped to 100, got %f", score)
	}
}

func TestScoreModel_NilScoreFieldSkipped(t *testing.T) {
	// A CapabilityScore with nil Score should be ignored.
	scores := []CapabilityScore{
		{ModelID: 1, Dimension: "coding", Score: nil},
	}
	score := ScoreModel(1, UseCaseCoding, PriorityQuality, scores)
	if score != 0 {
		t.Errorf("expected 0 when all scores are nil, got %f", score)
	}
}

func TestPrimaryDimensions(t *testing.T) {
	dims := PrimaryDimensions(UseCaseCoding)
	if len(dims) != 2 || dims[0] != "coding" || dims[1] != "reasoning" {
		t.Errorf("unexpected coding dims: %v", dims)
	}

	dims = PrimaryDimensions(UseCaseAgentic)
	if len(dims) != 3 {
		t.Errorf("expected 3 agentic dims, got %d: %v", len(dims), dims)
	}

	// Unknown use case falls back to general.
	dims = PrimaryDimensions(UseCase("unknown"))
	generalDims := PrimaryDimensions(UseCaseGeneral)
	if len(dims) != len(generalDims) {
		t.Errorf("unknown use case should fall back to general dims")
	}
}

func TestDimensionWeights_PrimaryHigherThanSecondary(t *testing.T) {
	// For any use case + priority, primary dimension weights must be higher
	// than secondary dimension weights.
	cases := []struct {
		uc  UseCase
		pri Priority
	}{
		{UseCaseCoding, PriorityQuality},
		{UseCaseFinance, PriorityCost},
		{UseCaseAgentic, PriorityBalanced},
		{UseCaseWriting, PrioritySpeed},
		{UseCaseReasoning, PriorityQuality},
	}
	for _, tc := range cases {
		w := dimensionWeights(tc.uc, tc.pri)
		primaryDims := PrimaryDimensions(tc.uc)
		primarySet := map[string]bool{}
		for _, d := range primaryDims {
			primarySet[d] = true
		}

		var minPrimary, maxSecondary float64
		minPrimary = 1e9
		for dim, weight := range w {
			if primarySet[dim] {
				if weight < minPrimary {
					minPrimary = weight
				}
			} else {
				if weight > maxSecondary {
					maxSecondary = weight
				}
			}
		}
		if maxSecondary > 0 && minPrimary <= maxSecondary {
			t.Errorf("use_case=%s priority=%s: min primary weight (%.3f) should exceed max secondary (%.3f)",
				tc.uc, tc.pri, minPrimary, maxSecondary)
		}
	}
}

func TestDimensionWeights_AllDimensionsCovered(t *testing.T) {
	// dimensionWeights must return an entry for every DimensionBenchmarks key.
	w := dimensionWeights(UseCaseCoding, PriorityBalanced)
	for dim := range DimensionBenchmarks {
		if _, ok := w[dim]; !ok {
			t.Errorf("dimensionWeights missing entry for dimension %q", dim)
		}
	}
}

func TestDimensionWeights_QualityPriorityDoublesWeights(t *testing.T) {
	// PriorityQuality should double primary weights relative to PriorityBalanced.
	wBalanced := dimensionWeights(UseCaseCoding, PriorityBalanced)
	wQuality := dimensionWeights(UseCaseCoding, PriorityQuality)
	primaryDims := PrimaryDimensions(UseCaseCoding)
	for _, dim := range primaryDims {
		if wQuality[dim] != wBalanced[dim]*2.0 {
			t.Errorf("dim %q: quality weight %.3f != balanced*2 (%.3f)", dim, wQuality[dim], wBalanced[dim]*2.0)
		}
	}
}
