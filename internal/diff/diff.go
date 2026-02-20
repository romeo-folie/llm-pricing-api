package diff

import (
	"math"

	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/scraper"
)


// PriceDiff describes a detected change in a single price field for one model.
type PriceDiff struct {
	ModelSlug          string
	Field              models.PriceField
	OldValue           float64
	NewValue           float64
	PctChange          float64 // signed: positive = price increase
	Source             string
	UnderlyingProvider string // propagated from ScrapedModel; "" for direct sources
	NeedsReview        bool   // true when abs(PctChange) > 5%
}

// Diff computes price changes between current stored prices and freshly scraped data.
//
// storedModels and stored are used together to resolve model_id → slug for matching.
// stored should be pre-filtered by the caller to the source matching incoming.
// Models present in stored but absent from incoming are silently ignored.
// New models (slug not in stored) produce diffs with OldValue = 0 and NeedsReview = false.
func Diff(stored []models.Price, storedModels []models.Model, incoming []scraper.ScrapedModel) []PriceDiff {
	// Build slug → Price index from stored data.
	slugToPrice := make(map[string]models.Price, len(stored))
	if len(stored) > 0 && len(storedModels) > 0 {
		idToSlug := make(map[int]string, len(storedModels))
		for _, m := range storedModels {
			idToSlug[m.ID] = m.Slug
		}
		for _, p := range stored {
			if slug, ok := idToSlug[p.ModelID]; ok {
				slugToPrice[slug] = p
			}
		}
	}

	diffs := make([]PriceDiff, 0)
	for _, s := range incoming {
		current, exists := slugToPrice[s.Slug]
		if !exists {
			// New model — emit diffs only for non-zero prices (zero-priced scraper results
			// are skipped to avoid fabricated diffs for free/open-weight models or scraper bugs).
			const epsilon = 1e-12
			if s.InputCostPerToken > epsilon {
				diffs = append(diffs, newModelDiff(s.Slug, s.SourceName, s.UnderlyingProvider, models.PriceFieldInput, s.InputCostPerToken))
			}
			if s.OutputCostPerToken > epsilon {
				diffs = append(diffs, newModelDiff(s.Slug, s.SourceName, s.UnderlyingProvider, models.PriceFieldOutput, s.OutputCostPerToken))
			}
			continue
		}

		if d, changed := fieldDiff(s.Slug, s.SourceName, s.UnderlyingProvider, models.PriceFieldInput, current.InputCostPerToken, s.InputCostPerToken); changed {
			diffs = append(diffs, d)
		}
		if d, changed := fieldDiff(s.Slug, s.SourceName, s.UnderlyingProvider, models.PriceFieldOutput, current.OutputCostPerToken, s.OutputCostPerToken); changed {
			diffs = append(diffs, d)
		}
	}
	return diffs
}

func newModelDiff(slug, source, underlyingProvider string, field models.PriceField, newVal float64) PriceDiff {
	return PriceDiff{
		ModelSlug:          slug,
		Field:              field,
		OldValue:           0,
		NewValue:           newVal,
		PctChange:          1.0,
		Source:             source,
		UnderlyingProvider: underlyingProvider,
		NeedsReview:        false,
	}
}

func fieldDiff(slug, source, underlyingProvider string, field models.PriceField, oldVal, newVal float64) (PriceDiff, bool) {
	const epsilon = 1e-12
	if math.Abs(newVal-oldVal) < epsilon {
		return PriceDiff{}, false
	}

	var pct float64
	if math.Abs(oldVal) < epsilon {
		pct = 1.0
	} else {
		pct = (newVal - oldVal) / oldVal
	}

	return PriceDiff{
		ModelSlug:          slug,
		Field:              field,
		OldValue:           oldVal,
		NewValue:           newVal,
		PctChange:          pct,
		Source:             source,
		UnderlyingProvider: underlyingProvider,
		NeedsReview:        math.Abs(pct) > models.DiscrepancyThreshold,
	}, true
}
