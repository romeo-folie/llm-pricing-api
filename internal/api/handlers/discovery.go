package handlers

import (
	"fmt"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/api"
)

// defaultOpenAPISpecPath is the path to the OpenAPI 3.1 JSON document
// relative to the module root. Go's //go:embed directive cannot reference
// paths above the package directory (e.g. "../../../api/openapi.json"), so
// the spec is read from disk at request time instead of embedded at build time.
const defaultOpenAPISpecPath = "api/openapi.json"

// DiscoveryHandler serves the three public discovery endpoints:
//   - GET /openapi.json        — OpenAPI 3.1 specification
//   - GET /.well-known/ai-plugin.json — AI plugin manifest
//   - GET /llms.txt            — plain-text model price listing for agent context
type DiscoveryHandler struct {
	store       Store
	specPath    string // path to api/openapi.json; overrideable in tests
}

// NewDiscoveryHandler creates a DiscoveryHandler backed by the supplied pgx pool.
// The OpenAPI spec is read from the default path relative to the module root.
func NewDiscoveryHandler(db *pgxpool.Pool) *DiscoveryHandler {
	return &DiscoveryHandler{
		store:    NewPgxStore(db),
		specPath: defaultOpenAPISpecPath,
	}
}

// NewDiscoveryHandlerForTest creates a DiscoveryHandler with the supplied
// Store and a custom specPath. Exported so that external test packages can
// inject a mock store and point to the spec file without needing a live DB.
func NewDiscoveryHandlerForTest(store Store, specPath string) *DiscoveryHandler {
	return &DiscoveryHandler{store: store, specPath: specPath}
}

// GetOpenAPI serves GET /openapi.json.
// Returns the OpenAPI 3.1 JSON document read from disk with no authentication.
func (h *DiscoveryHandler) GetOpenAPI(c *fiber.Ctx) error {
	spec, err := os.ReadFile(h.specPath)
	if err != nil || len(spec) == 0 {
		return api.NewInternalError("OpenAPI specification is unavailable")
	}
	c.Set("Content-Type", "application/json")
	return c.Send(spec)
}

// GetAIPlugin serves GET /.well-known/ai-plugin.json.
// Returns the AI plugin manifest for agent discovery with no authentication.
func (h *DiscoveryHandler) GetAIPlugin(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"schema_version": "v1",
		"name_for_model": "llm_pricing",
		"description_for_model": "Access current and historical LLM token pricing from multiple" +
			" providers. Query prices by provider, modality, or model ID." +
			" Compare costs and track price changes over time.",
		"api": fiber.Map{
			"type": "openapi",
			"url":  "/openapi.json",
		},
	})
}

// GetLLMsTxt serves GET /llms.txt.
// Returns a plain-text listing of all current model prices, one per line, in
// the format:
//
//	{provider}/{slug}: input=${price_input}/1M output=${price_output}/1M
//
// Prices are expressed in per-million-token units for readability. The
// endpoint is public and requires no authentication.
func (h *DiscoveryHandler) GetLLMsTxt(c *fiber.Ctx) error {
	models, err := h.store.ListModelsForContext(c.Context(), 1000)
	if err != nil {
		return api.NewInternalError("failed to fetch model list")
	}

	var sb strings.Builder
	for _, m := range models {
		// Prices stored as cost-per-token; convert to per-million-token for display.
		fmt.Fprintf(&sb, "%s/%s: input=$%.4f/1M output=$%.4f/1M\n",
			m.Provider, m.Slug,
			m.PriceInput*1_000_000,
			m.PriceOutput*1_000_000,
		)
	}

	c.Set("Content-Type", "text/plain; charset=utf-8")
	return c.SendString(sb.String())
}
