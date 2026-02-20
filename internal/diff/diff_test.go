package diff

import (
	"testing"
	"time"

	"llm-pricing-api/internal/models"
	"llm-pricing-api/internal/scraper"
)

// helpers

func storedPrice(modelID int, input, output float64) models.Price {
	return models.Price{
		ModelID:            modelID,
		InputCostPerToken:  input,
		OutputCostPerToken: output,
		SourceID:           1,
		Confidence:         models.ConfidenceMedium,
	}
}

func storedModel(id int, slug, provider string) models.Model {
	return models.Model{ID: id, Slug: slug, Provider: provider, Modality: models.ModalityText}
}

func incoming(slug, provider string, input, output float64) scraper.ScrapedModel {
	return incomingFrom(slug, provider, "openrouter", input, output)
}

func incomingFrom(slug, provider, sourceName string, input, output float64) scraper.ScrapedModel {
	return scraper.ScrapedModel{
		Slug:               slug,
		Provider:           provider,
		InputCostPerToken:  input,
		OutputCostPerToken: output,
		SourceName:         sourceName,
		FetchedAt:          time.Now(),
	}
}

func incomingWithUnderlying(slug, provider, sourceName, underlyingProvider string, input, output float64) scraper.ScrapedModel {
	m := incomingFrom(slug, provider, sourceName, input, output)
	m.UnderlyingProvider = underlyingProvider
	return m
}

// tests

func TestDiff_NoStoredPrices_NewModelIsNotFlaggedForReview(t *testing.T) {
	result := Diff(nil, nil, []scraper.ScrapedModel{
		incoming("openai/gpt-4", "openai", 0.00003, 0.00006),
	})

	if len(result) != 2 {
		t.Fatalf("want 2 diffs (input+output), got %d", len(result))
	}
	for _, d := range result {
		if d.OldValue != 0 {
			t.Errorf("field %s: want OldValue=0 for new model, got %f", d.Field, d.OldValue)
		}
		if d.NeedsReview {
			t.Errorf("field %s: new model should not need review", d.Field)
		}
	}
}

func TestDiff_PriceUnchanged_NoDiff(t *testing.T) {
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00003, 0.00006)}

	result := Diff(prices, models_, inc)

	if len(result) != 0 {
		t.Fatalf("want 0 diffs for unchanged price, got %d", len(result))
	}
}

func TestDiff_SmallPriceChange_NeedsReviewFalse(t *testing.T) {
	// 3% change — below the 5% threshold
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.0000309, 0.00006)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff (input only changed), got %d", len(result))
	}
	if result[0].NeedsReview {
		t.Error("3% change should not need review")
	}
	if result[0].Field != models.PriceFieldInput {
		t.Errorf("want field=%s, got %s", models.PriceFieldInput, result[0].Field)
	}
}

func TestDiff_LargePriceChange_NeedsReviewTrue(t *testing.T) {
	// 100% price increase — above the 5% threshold
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00006, 0.00006)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	if !result[0].NeedsReview {
		t.Error("100% change should need review")
	}
}

func TestDiff_BothFieldsChanged_TwoDiffs(t *testing.T) {
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}
	// Both input and output change by >5%
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00006, 0.00012)}

	result := Diff(prices, models_, inc)

	if len(result) != 2 {
		t.Fatalf("want 2 diffs, got %d", len(result))
	}
	fields := map[models.PriceField]bool{}
	for _, d := range result {
		fields[d.Field] = true
		if !d.NeedsReview {
			t.Errorf("field %s: 100%% change should need review", d.Field)
		}
	}
	if !fields[models.PriceFieldInput] || !fields[models.PriceFieldOutput] {
		t.Error("want diffs for both input and output fields")
	}
}

func TestDiff_ModelInDBButNotIncoming_Ignored(t *testing.T) {
	// Model exists in DB but scraper didn't return it — should not produce a diff
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}

	result := Diff(prices, models_, nil) // empty incoming

	if len(result) != 0 {
		t.Fatalf("want 0 diffs for model missing from incoming, got %d", len(result))
	}
}

func TestDiff_MultipleModels(t *testing.T) {
	models_ := []models.Model{
		storedModel(1, "openai/gpt-4", "openai"),
		storedModel(2, "anthropic/claude-3-opus", "anthropic"),
	}
	prices := []models.Price{
		storedPrice(1, 0.00003, 0.00006),
		storedPrice(2, 0.000015, 0.000075),
	}
	inc := []scraper.ScrapedModel{
		incoming("openai/gpt-4", "openai", 0.00003, 0.00006),    // unchanged
		incoming("anthropic/claude-3-opus", "anthropic", 0.00003, 0.000075), // input doubled
	}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff (claude input), got %d", len(result))
	}
	if result[0].ModelSlug != "anthropic/claude-3-opus" {
		t.Errorf("want diff for claude, got %s", result[0].ModelSlug)
	}
}

func TestDiff_ZeroOldPrice_PctChangeIsOne(t *testing.T) {
	// Stored price has input=0 (degenerate but defensive); should not divide by zero.
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0, 0.0002)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00003, 0.0002)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff (input changed from 0), got %d", len(result))
	}
	if result[0].PctChange != 1.0 {
		t.Errorf("want PctChange=1.0 when old=0, got %f", result[0].PctChange)
	}
	if !result[0].NeedsReview {
		t.Error("transition from 0 to non-zero should need review (pct=1.0 > threshold)")
	}
}

func TestDiff_NewModelWithZeroPrice_NoDiff(t *testing.T) {
	// Scraper returns a free/open-weight model with price=0 — should not emit a diff.
	result := Diff(nil, nil, []scraper.ScrapedModel{
		incoming("meta/llama-3", "meta", 0, 0),
	})
	if len(result) != 0 {
		t.Fatalf("want 0 diffs for zero-priced new model, got %d", len(result))
	}
}

func TestDiff_PriceDecrease_NegativePctChange_NeedsReview(t *testing.T) {
	// -50% decrease — signed PctChange must be negative and still flag for review.
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00006, 0.00012)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00003, 0.00012)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	if result[0].PctChange >= 0 {
		t.Errorf("want negative PctChange for price decrease, got %f", result[0].PctChange)
	}
	if !result[0].NeedsReview {
		t.Error("50%% decrease should need review")
	}
}

func TestDiff_ExactlyFivePercent_NeedsReviewFalse(t *testing.T) {
	// Threshold is strictly >, so exactly 5% must NOT flag for review.
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.0001, 0.0002)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.000105, 0.0002)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	if result[0].NeedsReview {
		t.Error("exactly 5% should NOT need review (threshold is strictly >)")
	}
}

func TestDiff_JustOverFivePercent_NeedsReviewTrue(t *testing.T) {
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.0001, 0.0002)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00010501, 0.0002)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	if !result[0].NeedsReview {
		t.Error("just over 5% should need review")
	}
}

func TestDiff_SourceAttribution_PropagatedToDiff(t *testing.T) {
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}
	inc := []scraper.ScrapedModel{incomingFrom("openai/gpt-4", "openai", "litellm", 0.00006, 0.00006)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	if result[0].Source != "litellm" {
		t.Errorf("want Source=%q, got %q", "litellm", result[0].Source)
	}
}

func TestDiff_UnderlyingProvider_PropagatedToNewModelDiff(t *testing.T) {
	inc := incomingWithUnderlying("together/llama-3", "together", "huggingface_inference_providers", "together-ai", 0.0001, 0.0002)
	result := Diff(nil, nil, []scraper.ScrapedModel{inc})

	if len(result) != 2 {
		t.Fatalf("want 2 diffs for new model, got %d", len(result))
	}
	for _, d := range result {
		if d.UnderlyingProvider != "together-ai" {
			t.Errorf("field %s: want UnderlyingProvider=%q, got %q", d.Field, "together-ai", d.UnderlyingProvider)
		}
	}
}

func TestDiff_UnderlyingProvider_PropagatedToFieldDiff(t *testing.T) {
	models_ := []models.Model{storedModel(1, "together/llama-3", "together")}
	prices := []models.Price{storedPrice(1, 0.0001, 0.0002)}
	inc := incomingWithUnderlying("together/llama-3", "together", "huggingface_inference_providers", "together-ai", 0.0002, 0.0002)

	result := Diff(prices, models_, []scraper.ScrapedModel{inc})

	if len(result) != 1 {
		t.Fatalf("want 1 diff (input changed), got %d", len(result))
	}
	if result[0].UnderlyingProvider != "together-ai" {
		t.Errorf("want UnderlyingProvider=%q, got %q", "together-ai", result[0].UnderlyingProvider)
	}
}

func TestDiff_UnderlyingProvider_EmptyForDirectSources(t *testing.T) {
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.00003, 0.00006)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00006, 0.00006)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	if result[0].UnderlyingProvider != "" {
		t.Errorf("want empty UnderlyingProvider for direct source, got %q", result[0].UnderlyingProvider)
	}
}

func TestDiff_PctChange_CalculatedCorrectly(t *testing.T) {
	models_ := []models.Model{storedModel(1, "openai/gpt-4", "openai")}
	prices := []models.Price{storedPrice(1, 0.0001, 0.0002)}
	inc := []scraper.ScrapedModel{incoming("openai/gpt-4", "openai", 0.00011, 0.0002)}

	result := Diff(prices, models_, inc)

	if len(result) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result))
	}
	// (0.00011 - 0.0001) / 0.0001 = 0.10 = 10%
	want := 0.10
	got := result[0].PctChange
	if got < want-0.001 || got > want+0.001 {
		t.Errorf("want PctChange≈%.3f, got %.3f", want, got)
	}
}
