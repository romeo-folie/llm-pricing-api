package intelligence

import "math"

// UseCase represents a use-case category for model recommendation scoring.
type UseCase string

const (
	UseCaseFinance   UseCase = "finance"
	UseCaseCoding    UseCase = "coding"
	UseCaseAgentic   UseCase = "agentic"
	UseCaseWriting   UseCase = "writing"
	UseCaseVideo     UseCase = "video"
	UseCaseReasoning UseCase = "reasoning"
	UseCaseGeneral   UseCase = "general"
)

// ValidUseCases is the set of recognised use-case values.
var ValidUseCases = map[UseCase]bool{
	UseCaseFinance:   true,
	UseCaseCoding:    true,
	UseCaseAgentic:   true,
	UseCaseWriting:   true,
	UseCaseVideo:     true,
	UseCaseReasoning: true,
	UseCaseGeneral:   true,
}

// Priority controls how capability vs cost/speed dimensions are weighted.
type Priority string

const (
	PriorityQuality  Priority = "quality"
	PriorityCost     Priority = "cost"
	PrioritySpeed    Priority = "speed"
	PriorityBalanced Priority = "balanced"
)

// ValidPriorities is the set of recognised priority values.
var ValidPriorities = map[Priority]bool{
	PriorityQuality:  true,
	PriorityCost:     true,
	PrioritySpeed:    true,
	PriorityBalanced: true,
}

// useCaseDimensions maps each use case to its primary capability dimensions.
var useCaseDimensions = map[UseCase][]string{
	UseCaseFinance:   {"finance", "quality"},
	UseCaseCoding:    {"coding", "reasoning"},
	UseCaseAgentic:   {"agentic", "tool_use", "reasoning"},
	UseCaseWriting:   {"writing", "instruction"},
	UseCaseVideo:     {"video"},
	UseCaseReasoning: {"reasoning", "quality"},
	UseCaseGeneral:   {"quality", "preference"},
}

// PrimaryDimensions returns the primary capability dimensions for a use case.
func PrimaryDimensions(uc UseCase) []string {
	if dims, ok := useCaseDimensions[uc]; ok {
		return dims
	}
	return useCaseDimensions[UseCaseGeneral]
}

// dimensionWeights computes the weight multiplier for each dimension based on
// the use case's primary dimensions and the priority modifier.
func dimensionWeights(uc UseCase, pri Priority) map[string]float64 {
	primaryDims := PrimaryDimensions(uc)
	primarySet := make(map[string]bool, len(primaryDims))
	for _, d := range primaryDims {
		primarySet[d] = true
	}

	weights := make(map[string]float64, len(DimensionBenchmarks))

	// Base weight: 1.0 for primary dimensions, 0.25 for others.
	for dim := range DimensionBenchmarks {
		if primarySet[dim] {
			weights[dim] = 1.0
		} else {
			weights[dim] = 0.25
		}
	}

	// Apply priority modifiers.
	switch pri {
	case PriorityQuality:
		for dim := range weights {
			if primarySet[dim] {
				weights[dim] *= 2.0
			} else {
				weights[dim] *= 0.5
			}
		}
	case PriorityCost:
		// Cost priority reduces emphasis on capability; callers apply price sorting.
		for dim := range weights {
			if primarySet[dim] {
				weights[dim] *= 0.75
			}
		}
	case PrioritySpeed:
		for dim := range weights {
			if primarySet[dim] {
				weights[dim] *= 0.5
			}
		}
	case PriorityBalanced:
		// All weights stay as-is.
	}

	return weights
}

// ScoreModel computes a composite capability score (0–100) for a model given
// its pre-fetched capability scores, use case, and priority.
// Returns 0 if no capability scores are available.
// ScoreModel does not call the DB — callers must pre-fetch scores.
func ScoreModel(modelID int, useCase UseCase, priority Priority, capScores []CapabilityScore) float64 {
	if len(capScores) == 0 {
		return 0
	}

	scoreByDim := make(map[string]float64, len(capScores))
	for _, cs := range capScores {
		if cs.Score != nil {
			scoreByDim[cs.Dimension] = *cs.Score
		}
	}

	weights := dimensionWeights(useCase, priority)

	var weightedSum, totalWeight float64
	for dim, w := range weights {
		score, ok := scoreByDim[dim]
		if !ok {
			continue
		}
		weightedSum += score * w
		totalWeight += w
	}

	if totalWeight == 0 {
		return 0
	}

	raw := weightedSum / totalWeight
	return math.Min(100, math.Max(0, raw))
}
