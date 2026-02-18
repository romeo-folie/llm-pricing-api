package worker

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
	TypeWebhookDeliver   = "webhook:deliver"
)
