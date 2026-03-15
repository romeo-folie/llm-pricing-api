package scraper

import (
	"context"
	"time"
)

// ScrapedModel is the normalised representation of a model and its pricing
// as returned by any scraper. It is intentionally separate from the models
// package to avoid circular imports: scrapers produce ScrapedModels, the
// reconciler consumes both ScrapedModels and models.Model.
type ScrapedModel struct {
	Slug               string
	Provider           string
	InputCostPerToken  float64
	OutputCostPerToken float64
	ContextWindow      *int // nil if not reported by source
	// Modality is the model's input/output type. Valid values match models.Modality:
	// "text", "multimodal", "image", "audio", "embedding".
	// The reconciler validates this before writing to the DB.
	Modality string
	// SourceName identifies the origin of this record. Expected values:
	// "openrouter", "litellm", "huggingface_inference_providers", "openai", "anthropic".
	SourceName string
	// UnderlyingProvider identifies the actual infrastructure provider when the
	// source is a pass-through aggregator (e.g. "together-ai" when SourceName is
	// "huggingface_inference_providers", or "together" when SourceName is "openrouter").
	// Empty string means the source IS the infrastructure provider (e.g. "litellm").
	UnderlyingProvider string
	FetchedAt          time.Time
}

// Scraper is implemented by every data source. Fetch must be safe to call
// concurrently and must propagate errors so the asynq worker can retry.
type Scraper interface {
	Fetch(ctx context.Context) ([]ScrapedModel, error)
}
