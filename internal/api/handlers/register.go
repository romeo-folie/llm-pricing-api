package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/middleware"
)

// Handlers holds shared dependencies used by all handler methods.
// Create one via New and pass it to Register* functions.
type Handlers struct {
	store Store
}

// New creates a Handlers instance backed by the provided Store.
// Pass a mock store in tests; pass NewPgxStore(db) in production.
func New(store Store) *Handlers {
	return &Handlers{store: store}
}

// RegisterPublic registers unauthenticated /v1 endpoints on the root Fiber app.
// These routes serve the public Compare page and require no API key — they are
// intentionally placed outside the auth-gated v1 group.
//
// Routes registered:
//
//	GET /v1/compare   — compare two models side-by-side
//	GET /v1/recommend — return top-ranked models for a use case
func RegisterPublic(app *fiber.App, db *pgxpool.Pool, rdb *redis.Client) {
	store := NewPgxStore(db)
	h := New(store)

	pub := app.Group("/v1")
	pub.Get("/compare", h.Compare)
	pub.Get("/recommend", h.Recommend)
}

// RegisterFree registers all Free-tier GET endpoint handlers on the v1 router.
// The db pool and rdb client are used to construct the production Store and are
// not used directly by handler functions.
//
// Routes registered:
//
//	GET /v1/models
//	GET /v1/models/:id
//	GET /v1/providers
//	GET /v1/changes/summary
//	GET /v1/changes
func RegisterFree(v1 fiber.Router, db *pgxpool.Pool, _ *redis.Client) {
	store := NewPgxStore(db)
	h := New(store)

	v1.Get("/models", h.ListModels)
	v1.Get("/models/:id", h.GetModel)
	v1.Get("/providers", h.ListProviders)
	v1.Get("/changes/summary", h.GetChangesSummary)
	v1.Get("/changes", h.ListChanges)
}

// RegisterDev registers previously-Developer+ endpoint handlers on the v1 router.
// Tier gating has been removed — all authenticated keys (including free-tier)
// can access these endpoints.
//
// Routes registered:
//
//	GET  /v1/models/:id/history
//	GET  /v1/context
//	POST /v1/ask
//
// Returns an error if the /v1/ask OTel instruments cannot be created.
func RegisterDev(v1 fiber.Router, db *pgxpool.Pool, _ *redis.Client) error {
	store := NewPgxStore(db)
	h := New(store)

	v1.Get("/models/:id/history", h.GetModelHistory)
	v1.Get("/context", h.GetContext)

	ask, err := NewAskHandler(store)
	if err != nil {
		return fmt.Errorf("create ask handler: %w", err)
	}
	v1.Post("/ask", ask.Ask)
	return nil
}

// RegisterDiscovery registers the public discovery endpoints on the root Fiber
// app. These routes are intentionally placed outside any auth group so they
// require no API key:
//
//   - GET /openapi.json              — OpenAPI 3.1 specification
//   - GET /.well-known/ai-plugin.json — AI plugin manifest
//   - GET /llms.txt                  — plain-text model price listing (Redis-cached, 30 min TTL)
func RegisterDiscovery(app *fiber.App, db *pgxpool.Pool, rdb *redis.Client) {
	dh := NewDiscoveryHandler(db, rdb)
	app.Get("/openapi.json", dh.GetOpenAPI)
	app.Get("/.well-known/ai-plugin.json", dh.GetAIPlugin)
	app.Get("/llms.txt", dh.GetLLMsTxt)
}

// RegisterSSE registers the SSE stream endpoint on the supplied v1 router.
// rdb is the Redis client used for Pub/Sub subscription, replay buffer access,
// and per-key connection limiting. Passing nil disables these features (heartbeat only).
//
// Route registered:
//
//	GET /v1/stream/changes  — Server-Sent Events stream (all authenticated tiers)
//
// Returns an error if the OTel instruments cannot be created.
func RegisterSSE(v1 fiber.Router, rdb *redis.Client) error {
	sse, err := NewSSEHandler(rdb)
	if err != nil {
		return fmt.Errorf("create SSE handler: %w", err)
	}
	v1.Get("/stream/changes", sse.StreamChanges)
	return nil
}

// RegisterFeedback registers the public POST /v1/feedback endpoint on the root
// Fiber app. No API key is required, but IP-based rate limiting is applied.
// If an Authorization header is present, the key hash is recorded.
//
// trustedProxies is passed to IPRateLimit so that the real end-user IP is
// resolved from X-Forwarded-For when the request arrives via a known proxy
// (e.g. the Next.js frontend proxy on Vercel). Without trusted proxies the
// rate limiter keys on the proxy server's IP, throttling all users together.
//
// Route registered:
//
//	POST /v1/feedback — submit recommendation feedback
func RegisterFeedback(app *fiber.App, db *pgxpool.Pool, rdb *redis.Client, log zerolog.Logger, trustedProxies ...string) {
	fs := NewFeedbackStore(db)
	fh := NewFeedbackHandler(fs)

	fb := app.Group("/v1")
	fb.Post("/feedback", middleware.IPRateLimit(rdb, log, trustedProxies...), fh.Create)
}

// RegisterAdminFeedback registers the admin feedback analytics endpoint.
// This route is registered inside the admin group (basic auth protected).
//
// Route registered:
//
//	GET /v1/admin/feedback — feedback analytics for a use case
func RegisterAdminFeedback(admin fiber.Router, db *pgxpool.Pool) {
	fs := NewFeedbackStore(db)
	ah := NewAdminFeedbackHandler(fs)
	admin.Get("/feedback", ah.GetAnalytics)
}

// RegisterPro registers Pro-tier endpoint handlers on the v1 router.
// All routes here are gated behind RequireTier("pro") so Free and Developer
// tier keys receive RFC 7807 403 responses.
//
// webhookSecretKey is the hex-encoded 32-byte AES-256-GCM key used to encrypt
// webhook secrets at rest. Pass cfg.WebhookSecretKey from config; if empty a
// random ephemeral key is generated (secrets won't survive restarts — a warning
// is logged).
//
// Routes registered:
//
//	POST   /v1/webhooks     — register a webhook URL
//	DELETE /v1/webhooks/:id — remove a webhook by ID
func RegisterPro(v1 fiber.Router, db *pgxpool.Pool, _ *redis.Client, webhookSecretKey string, log zerolog.Logger) {
	key, err := resolveWebhookKey(webhookSecretKey, log)
	if err != nil {
		// resolveWebhookKey only errors on malformed hex; treat as fatal misconfiguration.
		log.Fatal().Err(err).Msg("invalid WEBHOOK_SECRET_KEY")
	}

	ws := NewWebhookStore(db)
	wh := &WebhookHandler{store: ws, secretKey: key}

	v1.Post("/webhooks", middleware.RequireTier(middleware.TierPro), wh.Create)
	v1.Delete("/webhooks/:id", middleware.RequireTier(middleware.TierPro), wh.Delete)
}
