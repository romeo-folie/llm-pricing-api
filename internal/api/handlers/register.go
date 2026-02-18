package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

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

// RegisterFree registers all Free-tier GET endpoint handlers on the v1 router.
// The db pool and rdb client are used to construct the production Store and are
// not used directly by handler functions.
//
// Routes registered:
//
//	GET /v1/models
//	GET /v1/models/:id
//	GET /v1/providers
//	GET /v1/compare
//	GET /v1/changes
func RegisterFree(v1 fiber.Router, db *pgxpool.Pool, _ *redis.Client) {
	store := NewPgxStore(db)
	h := New(store)

	v1.Get("/models", h.ListModels)
	v1.Get("/models/:id", h.GetModel)
	v1.Get("/providers", h.ListProviders)
	v1.Get("/compare", h.Compare)
	v1.Get("/changes", h.ListChanges)
}

// RegisterDev registers Developer+ endpoint handlers on the v1 router.
// All routes here are gated behind RequireTier("developer") so Free-tier
// keys receive RFC 7807 403 responses.
//
// Routes registered:
//
//	GET /v1/models/:id/history
//	GET /v1/recommend
//	GET /v1/context
func RegisterDev(v1 fiber.Router, db *pgxpool.Pool, _ *redis.Client) {
	store := NewPgxStore(db)
	h := New(store)

	v1.Get("/models/:id/history", middleware.RequireTier(middleware.TierDeveloper), h.GetModelHistory)
	v1.Get("/recommend", middleware.RequireTier(middleware.TierDeveloper), h.Recommend)
	v1.Get("/context", middleware.RequireTier(middleware.TierDeveloper), h.GetContext)
}

// RegisterDiscovery registers the public discovery endpoints on the root Fiber
// app. These routes are intentionally placed outside any auth group so they
// require no API key:
//
//   - GET /openapi.json              — OpenAPI 3.1 specification
//   - GET /.well-known/ai-plugin.json — AI plugin manifest
//   - GET /llms.txt                  — plain-text model price listing
func RegisterDiscovery(app *fiber.App, db *pgxpool.Pool) {
	dh := NewDiscoveryHandler(db)
	app.Get("/openapi.json", dh.GetOpenAPI)
	app.Get("/.well-known/ai-plugin.json", dh.GetAIPlugin)
	app.Get("/llms.txt", dh.GetLLMsTxt)
}

// RegisterSSE registers the SSE stream endpoint on the supplied v1 router.
// The route is protected by the Developer-tier gate:
//
//	GET /v1/stream/changes  — Server-Sent Events stream (Developer+ tier)
//
// Returns an error if the OTel UpDownCounter cannot be created.
func RegisterSSE(v1 fiber.Router) error {
	sse, err := NewSSEHandler()
	if err != nil {
		return fmt.Errorf("create SSE handler: %w", err)
	}
	v1.Get("/stream/changes", middleware.RequireTier(middleware.TierDeveloper), sse.StreamChanges)
	return nil
}

// RegisterPro registers Pro-tier endpoint handlers on the v1 router.
// All routes here are gated behind RequireTier("pro") so Free and Developer
// tier keys receive RFC 7807 403 responses.
//
// Routes registered:
//
//	POST   /v1/webhooks     — register a webhook URL
//	DELETE /v1/webhooks/:id — remove a webhook by ID
func RegisterPro(v1 fiber.Router, db *pgxpool.Pool, _ *redis.Client) {
	ws := NewWebhookStore(db)
	wh := &WebhookHandler{store: ws}

	v1.Post("/webhooks", middleware.RequireTier(middleware.TierPro), wh.Create)
	v1.Delete("/webhooks/:id", middleware.RequireTier(middleware.TierPro), wh.Delete)
}
