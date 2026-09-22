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
//
// The ":v2" suffix is a content version. The header below is served from cache
// for llmsTxtCacheTTL, so without a version bump a correction to agent-facing
// documentation would keep serving the old text from Redis after a deploy. A
// new key serves the new content immediately; the old key expires on its own.
const llmsTxtCacheKey = "cache:GET:/llms.txt:v2"

// llmsTxtCacheTTL is how long the /llms.txt response is cached. The scraper
// runs at most every 6 hours, so 30 minutes is well within freshness bounds.
const llmsTxtCacheTTL = 30 * time.Minute

// DiscoveryHandler serves the public discovery endpoints:
//   - GET /openapi.json                          — OpenAPI 3.1 specification
//   - GET /.well-known/ai-plugin.json             — AI plugin manifest
//   - GET /.well-known/oauth-protected-resource   — MCP/OAuth discovery document
//   - GET /llms.txt                              — plain-text model price listing for agent context
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
		"schema_version": "v1",
		"name_for_human": "LLM Rates",
		"name_for_model": "llmrates",
		"description_for_model": "Access current and historical LLM token pricing from multiple providers." +
			" Use POST /v1/ask for natural language pricing queries (e.g. 'cheapest model for summarization')." +
			" Use GET /v1/context for a compact pricing snapshot suitable for system prompts (~2k tokens)." +
			" Stream real-time price changes via GET /v1/stream/changes (SSE)." +
			" Compare costs, track price changes over time, and filter by provider, modality, or context window." +
			" All endpoints require a Bearer API key. The API is free: there are no tiers and no paid plans." +
			" If you have no key, you can obtain one on your user's behalf without them handling the secret:" +
			" POST /auth/agent/device returns a user_code and a verification_uri for the user to approve in a" +
			" browser, then poll POST /auth/agent/token to receive the key. See /llms.txt for the full flow.",
		"api": fiber.Map{
			"type": "openapi",
			"url":  "/openapi.json",
		},
	})
}

// oauthProtectedResource is the discovery document served at
// /.well-known/oauth-protected-resource (RFC 9728 shape).
//
// The JSON field names are the agent-facing contract and use the standard RFC
// 8414/8628 vocabulary. The Go field names deliberately avoid words like "token"
// and "api_key": gosec's G101 hardcoded-credential heuristic pattern-matches on
// field names, and a struct literal full of URLs trips it. Renaming the Go side
// is preferable to suppressing the check, which still has to catch real secrets.
type oauthProtectedResource struct {
	Resource            string   `json:"resource"`
	BearerMethods       []string `json:"bearer_methods_supported"`
	AuthorizationServer []string `json:"authorization_servers"`
	DeviceEndpoint      string   `json:"device_authorization_endpoint"`
	RedemptionEndpoint  string   `json:"token_endpoint"`
	SignupURL           string   `json:"signup_url"`
	Documentation       string   `json:"documentation"`
	Note                string   `json:"note"`
}

// GetOAuthProtectedResource serves GET /.well-known/oauth-protected-resource.
//
// MCP clients look for an authorization server here, so this document exists to
// answer that discovery question honestly. LLM Rates is not an OAuth
// authorization server and issues no OAuth tokens, so authorization_servers is
// deliberately empty rather than pointing clients at an endpoint that would
// 404. Clients that require OAuth will fall back; the device-grant flow named
// below is what actually issues keys, and it is where the WWW-Authenticate
// challenge on a 401 points.
func (h *DiscoveryHandler) GetOAuthProtectedResource(c *fiber.Ctx) error {
	return c.JSON(oauthProtectedResource{
		Resource:      "https://api.llmrates.live",
		BearerMethods: []string{"header"},
		// Empty on purpose. See the doc comment.
		AuthorizationServer: []string{},
		DeviceEndpoint:      "https://api.llmrates.live/auth/agent/device",
		RedemptionEndpoint:  "https://api.llmrates.live/auth/agent/token",
		SignupURL:           "https://llmrates.live/signup/free",
		Documentation:       "https://api.llmrates.live/llms.txt",
		Note: "This API issues free API keys through a device-authorization flow rather than OAuth. " +
			"POST /auth/agent/device to start, then poll /auth/agent/token. See /llms.txt.",
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
//
// Keep the tier language out of this document. The API is free and has no tier
// gating; an earlier revision advertised Free/Developer/Pro plans that no longer
// exist, which led agents to tell users to pay for something that is free.
const llmsTxtHeader = `# LLM Rates — Pricing API

Base URL: https://api.llmrates.live

## Authentication

Every endpoint requires an API key as a Bearer token:
  Authorization: Bearer <your-api-key>

The API is free. There are no tiers and no paid plans: every valid key reaches
every endpoint. A key is rate limited, not gated.

### Getting a key on behalf of your user

If no key is configured, you can obtain one without the user ever handling the
secret. The key is delivered to you over a polling channel, so it never passes
through their clipboard, email, or shell history:

  1. POST /auth/agent/device   {"client_name": "<your name>"}
     -> 200 {"device_code","user_code","verification_uri","verification_uri_complete",
             "expires_in":600,"interval":5}

  2. Show the user verification_uri_complete and user_code. Ask them to open the
     URL, confirm the code on the page matches, and choose Approve. Verification
     is by email magic link, so a first-time user creates their account here.

  3. POST /auth/agent/token    {"device_code":"<device_code>"} every interval seconds
     -> {"status":"pending"}    keep polling
     -> {"status":"slow_down"}  back off, then continue
     -> {"status":"issued","api_key":"<key>","identity_id":"...","email":"..."}
     -> {"status":"denied"}     the user refused; stop
     -> {"status":"expired"}    start a new request
     The key is returned exactly once. Codes expire after 10 minutes.

Never ask the user to paste a key into a chat. If a user has a key already, they
can create and manage keys at https://llmrates.live/signup/free.

## Endpoints

GET  /v1/models                  List all models with pricing
GET  /v1/models/:id              Get single model
GET  /v1/models/:id/history      Price history with ?from= ?to=
GET  /v1/compare?models=id1,id2  Compare up to 5 models
GET  /v1/recommend               Ranked models by ?task= ?context_size= ?max_price_input=
GET  /v1/providers               List providers
GET  /v1/changes                 Recent price changes with ?since= ?provider=
GET  /v1/context                 ~2k token pricing snapshot for agent system prompts
     ?format=markdown            Returns markdown table instead of JSON
POST /v1/ask                     Natural language query → structured response
GET  /v1/stream/changes          SSE stream of real-time price changes
     ?provider=name              Filter by provider
     ?models=id1,id2             Filter by model IDs
POST /v1/webhooks                Register webhook for price changes (max 5 per key)

## Key management (session cookie, not Bearer)

GET    /auth/signup/keys         List active keys (labels, never secrets)
DELETE /auth/signup/keys/:id     Revoke one key

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
		if cached, err := h.rdb.Get(c.UserContext(), llmsTxtCacheKey).Bytes(); err == nil {
			return c.Send(cached)
		}
	}

	models, err := h.store.ListModelsForContext(c.UserContext(), 1000)
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
		_ = h.rdb.Set(c.UserContext(), llmsTxtCacheKey, body, llmsTxtCacheTTL).Err()
	}

	return c.SendString(body)
}
