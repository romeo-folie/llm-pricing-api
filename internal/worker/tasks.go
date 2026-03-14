package worker

import "llm-pricing-api/internal/webhooks"

// Task name constants for asynq job registration.
// These strings are used as the task type identifier when enqueuing
// and routing scraper jobs through the Redis-backed asynq queue.
const (
	TaskOpenRouterScrape  = "scrape:openrouter"
	TaskLiteLLMScrape     = "scrape:litellm"
	TaskHuggingFaceScrape = "scrape:huggingface"
	// TaskOpenAIScrape triggers the HTML scraper for developers.openai.com/pricing.
	TaskOpenAIScrape = "scrape:openai"
)

// TypeWebhookDeliver re-exports the canonical constant from internal/webhooks
// for callers that already import the worker package (e.g. cmd/worker/main.go).
// The canonical definition lives in internal/webhooks to avoid import cycles.
const TypeWebhookDeliver = webhooks.TypeWebhookDeliver
