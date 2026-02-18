package worker

// Task name constants for asynq job registration.
// These strings are used as the task type identifier when enqueuing
// and routing scraper jobs through the Redis-backed asynq queue.
const (
	TaskOpenRouterScrape = "scrape:openrouter"
	TaskLiteLLMScrape    = "scrape:litellm"
	TaskOpenAIScrape     = "scrape:openai"
	TaskAnthropicScrape  = "scrape:anthropic"
	TaskGoogleScrape     = "scrape:google"
	TaskMistralScrape    = "scrape:mistral"
	TaskAmazonScrape     = "scrape:amazon"
)
