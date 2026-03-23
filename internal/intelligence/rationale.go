package intelligence

import (
	"fmt"
	"sort"
	"time"
)

// BenchmarkCitation carries data needed for rationale and response.
type BenchmarkCitation struct {
	Name        string    `json:"name"`
	Score       float64   `json:"score"`
	SourceURL   string    `json:"source_url"`
	Confidence  string    `json:"confidence"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// GenerateRationale produces a 1–2 sentence deterministic explanation for why
// a model is recommended for the given use case and priority.
// Template-based — no LLM calls, fully deterministic and auditable.
func GenerateRationale(
	modelSlug string,
	capScores []CapabilityScore,
	citations []BenchmarkCitation,
	useCase UseCase,
	priority Priority,
) string {
	if len(capScores) == 0 {
		return "No benchmark data available; ranked by price."
	}

	type dimScore struct {
		Dimension  string
		Score      float64
		Confidence string
	}
	var valid []dimScore
	for _, cs := range capScores {
		if cs.Score != nil {
			valid = append(valid, dimScore{
				Dimension:  cs.Dimension,
				Score:      *cs.Score,
				Confidence: cs.Confidence,
			})
		}
	}

	if len(valid) == 0 {
		return "No benchmark data available; ranked by price."
	}

	// Rank by weighted importance for this use case + priority, then by raw score desc.
	weights := dimensionWeights(useCase, priority)
	sort.Slice(valid, func(i, j int) bool {
		wi := valid[i].Score * weights[valid[i].Dimension]
		wj := valid[j].Score * weights[valid[j].Dimension]
		return wi > wj
	})

	// Take top 1–2 dimensions.
	topN := 2
	if len(valid) < topN {
		topN = len(valid)
	}
	top := valid[:topN]

	rationale := fmt.Sprintf(
		"Ranks highly for %s tasks based on %s score of %.1f (confidence: %s).",
		string(useCase), top[0].Dimension, top[0].Score, top[0].Confidence,
	)

	if topN > 1 {
		rationale += fmt.Sprintf(
			" Also strong in %s (%.1f).",
			top[1].Dimension, top[1].Score,
		)
	}

	// Append top citation if available.
	if len(citations) > 0 {
		c := citations[0]
		rationale += fmt.Sprintf(
			" Benchmark: %s (%.1f, %s, %s).",
			c.Name, c.Score, c.SourceURL, c.EvaluatedAt.Format("2006-01"),
		)
	}

	return rationale
}
