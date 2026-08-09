package worker

import "llm-pricing-api/internal/webhooks"

// Task name constants for asynq job registration.
// These strings are used as the task type identifier when enqueuing
// and routing scraper jobs through the Redis-backed asynq queue.
const (
	TaskOpenRouterScrape  = "scrape:openrouter"
	TaskLiteLLMScrape     = "scrape:litellm"
	TaskHuggingFaceScrape = "scrape:huggingface"
	// TaskOpenAIScrape triggers the HTML scraper for https://developers.openai.com/api/docs/pricing?latest-pricing=standard
	TaskOpenAIScrape = "scrape:openai"
	// TaskAnthropicScrape triggers the HTML scraper for
	// https://platform.claude.com/docs/en/about-claude/pricing
	TaskAnthropicScrape = "scrape:anthropic"
	// TaskGeminiScrape triggers the HTML scraper for https://ai.google.dev/gemini-api/docs/pricing
	TaskGeminiScrape = "scrape:gemini"

	// Benchmark scraper tasks — daily cron jobs that fetch scores from
	// external leaderboards and upsert into model_benchmark_scores.
	TaskSWEBenchScrape      = "benchmark:swebench"
	TaskLiveCodeBenchScrape = "benchmark:livecodebench"
	TaskChatbotArenaScrape  = "benchmark:chatbot_arena"

	// Deprecated task-type names. These are the wire values used before the
	// bfcl → swebench and huggingface_llm → livecodebench renames. Handlers stay
	// registered for them so that tasks already sitting in Redis at deploy time
	// still dispatch instead of being archived as "handler not found".
	//
	// Safe to delete once no queued task predates the rename — one cron cycle
	// (24h) after the deploy that introduced it.
	TaskBFCLScrapeDeprecated           = "benchmark:bfcl"
	TaskHuggingFaceLLMScrapeDeprecated = "benchmark:huggingface_llm"

	// Intelligence recomputation tasks — run after benchmark scrapes
	// to keep capability scores and freshness indicators current.
	TaskRecomputeCapabilityScores = "intelligence:recompute_capability_scores"
	TaskStalenessCheck            = "intelligence:staleness_check"
)

// TypeWebhookDeliver re-exports the canonical constant from internal/webhooks
// for callers that already import the worker package (e.g. cmd/worker/main.go).
// The canonical definition lives in internal/webhooks to avoid import cycles.
const TypeWebhookDeliver = webhooks.TypeWebhookDeliver
