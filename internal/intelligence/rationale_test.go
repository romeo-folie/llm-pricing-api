package intelligence

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateRationale_EmptyScores(t *testing.T) {
	r := GenerateRationale("openai/gpt-4o", nil, nil, UseCaseCoding, PriorityQuality)
	if r != "No benchmark data available; ranked by price." {
		t.Errorf("unexpected fallback rationale: %s", r)
	}
}

func TestGenerateRationale_NilScoreFields(t *testing.T) {
	scores := []CapabilityScore{{ModelID: 1, Dimension: "coding", Score: nil}}
	r := GenerateRationale("openai/gpt-4o", scores, nil, UseCaseCoding, PriorityQuality)
	if r != "No benchmark data available; ranked by price." {
		t.Errorf("unexpected fallback rationale: %s", r)
	}
}

func TestGenerateRationale_WithScores(t *testing.T) {
	scores := makeCapScores(1, map[string]float64{"coding": 90.5, "reasoning": 85.3, "quality": 80.0})
	r := GenerateRationale("openai/gpt-4o", scores, nil, UseCaseCoding, PriorityQuality)
	if !strings.Contains(r, "coding tasks") {
		t.Errorf("rationale should mention use case: %s", r)
	}
	if !strings.Contains(r, "confidence: high") {
		t.Errorf("rationale should include confidence: %s", r)
	}
}

func TestGenerateRationale_WithCitations(t *testing.T) {
	scores := makeCapScores(1, map[string]float64{"coding": 90, "reasoning": 85})
	citations := []BenchmarkCitation{{
		Name: "SWE-bench Verified", Score: 72.5, SourceURL: "https://example.com/swe-bench",
		Confidence: "high", EvaluatedAt: time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
	}}
	r := GenerateRationale("openai/gpt-4o", scores, citations, UseCaseCoding, PriorityQuality)
	if !strings.Contains(r, "SWE-bench Verified") {
		t.Errorf("rationale should cite benchmark: %s", r)
	}
	if !strings.Contains(r, "2025-03") {
		t.Errorf("rationale should include evaluation date: %s", r)
	}
}

func TestGenerateRationale_SingleDimension(t *testing.T) {
	scores := makeCapScores(1, map[string]float64{"video": 78.2})
	r := GenerateRationale("google/gemini-2.0", scores, nil, UseCaseVideo, PriorityBalanced)
	if !strings.Contains(r, "video tasks") {
		t.Errorf("rationale should mention video use case: %s", r)
	}
	if strings.Contains(r, "Also strong") {
		t.Errorf("single-dimension rationale should not have Also strong: %s", r)
	}
}
