package handlers

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"llm-pricing-api/internal/api"
)

// openAPISpec holds the OpenAPI 3.1 JSON document embedded at build time.
// Embedding at compile time eliminates runtime file I/O and working-directory
// dependencies, ensuring the spec is always available regardless of where the
// binary is launched from (e.g. Docker, Railway).
//
//go:embed static/openapi.json
var openAPISpec []byte

// llmsTxtCacheKey is the Redis key for the cached /llms.txt response body.
const llmsTxtCacheKey = "cache:GET:/llms.txt"

// llmsTxtCacheTTL is how long the /llms.txt response is cached. The scraper
// runs at most every 6 hours, so 30 minutes is well within freshness bounds.
const llmsTxtCacheTTL = 30 * time.Minute

// DiscoveryHandler serves the three public discovery endpoints:
//   - GET /openapi.json              — OpenAPI 3.1 specification
//   - GET /.well-known/ai-plugin.json — AI plugin manifest
//   - GET /llms.txt                  — plain-text model price listing for agent context
type DiscoveryHandler struct {
	store Store
	rdb   *redis.Client // nil disables caching (tests, local dev without Redis)
}

// NewDiscoveryHandler creates a DiscoveryHandler backed by the supplied pgx pool
// and Redis client. Pass nil for rdb to disable response caching.
func NewDiscoveryHandler(db *pgxpool.Pool, rdb *redis.Client) *DiscoveryHandler {
	return &DiscoveryHandler{store: NewPgxStore(db), rdb: rdb}
}

// NewDiscoveryHandlerForTest creates a DiscoveryHandler with the supplied Store
// and Redis client. Pass nil for rdb to disable caching in tests.
// Exported so external test packages can inject mock dependencies without a live DB.
func NewDiscoveryHandlerForTest(store Store, rdb *redis.Client) *DiscoveryHandler {
	return &DiscoveryHandler{store: store, rdb: rdb}
}

// GetOpenAPI serves GET /openapi.json.
// Returns the compile-time embedded OpenAPI 3.1 JSON document.
func (h *DiscoveryHandler) GetOpenAPI(c *fiber.Ctx) error {
	c.Set("Content-Type", "application/json")
	return c.Send(openAPISpec)
}

// GetAIPlugin serves GET /.well-known/ai-plugin.json.
// Returns the AI plugin manifest for agent discovery with no authentication.
func (h *DiscoveryHandler) GetAIPlugin(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"schema_version":  "v1",
		"name_for_human":  "LLM Rates",
		"name_for_model":  "llmrates",
		"description_for_model": "Access current and historical LLM token pricing from multiple providers." +
			" Use POST /v1/ask for natural language pricing queries (e.g. 'cheapest model for summarization')." +
			" Use GET /v1/context for a compact pricing snapshot suitable for system prompts (~2k tokens)." +
			" Stream real-time price changes via GET /v1/stream/changes (SSE)." +
			" Compare costs, track price changes over time, and filter by provider, modality, or context window." +
			" All endpoints require a Bearer API key." +
			" Developer tier required for /v1/ask, /v1/context, and /v1/stream/changes.",
		"api": fiber.Map{
			"type": "openapi",
			"url":  "/openapi.json",
		},
	})
}

// llmsTxtHeader is the static header prepended to every /llms.txt response.
// It describes the API, authentication requirements, available endpoints,
// and example requests so that agents can bootstrap context without prior knowledge.
//
// The base URL is intentionally hardcoded to the production address — /llms.txt
// is agent-facing documentation that describes the public API, not the current
// deployment. Staging and local environments serve the same content so agents
// always reference the canonical production endpoint.
const llmsTxtHeader = `# LLM Rates — Pricing API

Base URL: https://api.llmrates.live

## Authentication

All endpoints require an API key as a Bearer token:
  Authorization: Bearer <your-api-key>

Tiers: Free (100 req/day), Developer ($15/mo, 10k req/day), Pro ($50/mo, unlimited + webhooks)

## Endpoints

GET  /v1/models                  List all models with pricing (Free)
GET  /v1/models/:id              Get single model (Free)
GET  /v1/models/:id/history      Price history with ?from= ?to= (Developer+)
GET  /v1/compare?models=id1,id2  Compare up to 5 models (Free)
GET  /v1/recommend               Ranked models by ?task= ?context_size= ?max_price_input= (Developer+)
GET  /v1/providers               List providers (Free)
GET  /v1/changes                 Recent price changes with ?since= ?provider= (Free)
GET  /v1/context                 ~2k token pricing snapshot for agent system prompts (Developer+)
     ?format=markdown            Returns markdown table instead of JSON
POST /v1/ask                     Natural language query → structured response (Developer+)
GET  /v1/stream/changes          SSE stream of real-time price changes (Developer+)
     ?provider=name              Filter by provider
     ?models=id1,id2             Filter by model IDs
POST /v1/webhooks                Register webhook for price changes (Pro)

## Example Requests

# List all OpenAI models
curl -H "Authorization: Bearer $KEY" https://api.llmrates.live/v1/models?provider=openai

# Natural language query
curl -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"query":"cheapest model for summarization under $5"}' \
  https://api.llmrates.live/v1/ask

# Get pricing snapshot for agent context (JSON)
curl -H "Authorization: Bearer $KEY" https://api.llmrates.live/v1/context

# Get pricing snapshot as markdown table
curl -H "Authorization: Bearer $KEY" https://api.llmrates.live/v1/context?format=markdown

# Stream price changes
curl -H "Authorization: Bearer $KEY" -H "Accept: text/event-stream" \
  https://api.llmrates.live/v1/stream/changes

## Current Pricing

`

// GetLLMsTxt serves GET /llms.txt.
// Returns a plain-text document containing a static header describing the API
// (authentication, all endpoints, and example requests) followed by a dynamic
// listing of all current model prices, one per line, in the format:
//
//	{provider}/{slug}: input=${price_input}/1M output=${price_output}/1M
//
// Prices are expressed in per-million-token units for readability. The
// endpoint is public and requires no authentication.
func (h *DiscoveryHandler) GetLLMsTxt(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/plain; charset=utf-8")
	c.Set(fiber.HeaderCacheControl, fmt.Sprintf("max-age=%d, public", int(llmsTxtCacheTTL.Seconds())))

	// Cache hit: serve from Redis without touching the DB.
	if h.rdb != nil {
		if cached, err := h.rdb.Get(c.Context(), llmsTxtCacheKey).Bytes(); err == nil {
			return c.Send(cached)
		}
	}

	models, err := h.store.ListModelsForContext(c.Context(), 1000)
	if err != nil {
		return api.NewInternalError("failed to fetch model list")
	}

	var sb strings.Builder
	sb.WriteString(llmsTxtHeader)
	for _, m := range models {
		// Prices stored as cost-per-token; convert to per-million-token for display.
		fmt.Fprintf(&sb, "%s/%s: input=$%.4f/1M output=$%.4f/1M\n",
			m.Provider, m.Slug,
			m.PriceInput*1_000_000,
			m.PriceOutput*1_000_000,
		)
	}

	body := sb.String()

	// Best-effort cache write; a Redis failure must not degrade the response.
	if h.rdb != nil {
		_ = h.rdb.Set(c.Context(), llmsTxtCacheKey, body, llmsTxtCacheTTL).Err()
	}

	return c.SendString(body)
}
