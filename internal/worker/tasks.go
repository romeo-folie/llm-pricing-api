package worker

import "llm-pricing-api/internal/webhooks"

// Task name constants for asynq job registration.
// These strings are used as the task type identifier when enqueuing
// and routing scraper jobs through the Redis-backed asynq queue.
//
// OpenAI, Anthropic, Google, Mistral, and Amazon provider HTML scrapers
// were removed: their pricing pages are JavaScript SPAs and cannot be
// reliably scraped with a plain HTTP GET. OpenRouter and LiteLLM cover
// the same models via machine-readable JSON endpoints.
const (
	TaskOpenRouterScrape = "scrape:openrouter"
	TaskLiteLLMScrape    = "scrape:litellm"
)

// TypeWebhookDeliver re-exports the canonical constant from internal/webhooks
// for callers that already import the worker package (e.g. cmd/worker/main.go).
// The canonical definition lives in internal/webhooks to avoid import cycles.
const TypeWebhookDeliver = webhooks.TypeWebhookDeliver
