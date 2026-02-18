// Package handlers contains Fiber HTTP handler implementations for the
// LLM pricing REST API endpoints.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/middleware"
)

// WebhookRecord is the DB row representation for a registered webhook.
type WebhookRecord struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// createWebhookResponse is the 201 body returned to the caller on successful
// webhook registration. The secret is returned exactly once and never again.
type createWebhookResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	Secret    string    `json:"secret"`
}

// WebhookStore abstracts all DB operations required by WebhookHandler.
type WebhookStore interface {
	// CreateWebhook inserts a new webhook registration and returns the created record.
	// secret is stored as plaintext in the DB so the reconciler can pass it
	// to the delivery worker.
	CreateWebhook(ctx context.Context, apiKeyHash, webhookURL, secret string) (WebhookRecord, error)

	// DeleteWebhook soft-deletes a webhook by ID, scoped to the given apiKeyHash
	// so that a key cannot delete another key's webhook.
	// Returns ErrWebhookNotFound if no matching active row exists.
	DeleteWebhook(ctx context.Context, id, apiKeyHash string) error
}

// ErrWebhookNotFound is returned by DeleteWebhook when no matching row is found.
var ErrWebhookNotFound = fmt.Errorf("webhook not found")

// pgxWebhookStore is the PostgreSQL-backed implementation of WebhookStore.
type pgxWebhookStore struct {
	db *pgxpool.Pool
}

// NewWebhookStore returns a WebhookStore backed by the provided connection pool.
func NewWebhookStore(db *pgxpool.Pool) WebhookStore {
	return &pgxWebhookStore{db: db}
}

func (s *pgxWebhookStore) CreateWebhook(ctx context.Context, apiKeyHash, webhookURL, secret string) (WebhookRecord, error) {
	var rec WebhookRecord
	err := s.db.QueryRow(ctx, `
		INSERT INTO webhooks (api_key_hash, url, secret)
		VALUES ($1, $2, $3)
		RETURNING id, url, created_at
	`, apiKeyHash, webhookURL, secret).Scan(&rec.ID, &rec.URL, &rec.CreatedAt)
	if err != nil {
		return WebhookRecord{}, fmt.Errorf("create webhook: %w", err)
	}
	return rec, nil
}

func (s *pgxWebhookStore) DeleteWebhook(ctx context.Context, id, apiKeyHash string) error {
	ct, err := s.db.Exec(ctx, `
		UPDATE webhooks
		SET deleted_at = NOW()
		WHERE id = $1
		  AND api_key_hash = $2
		  AND deleted_at IS NULL
	`, id, apiKeyHash)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrWebhookNotFound
	}
	return nil
}

// WebhookHandler handles POST /v1/webhooks and DELETE /v1/webhooks/:id.
type WebhookHandler struct {
	store WebhookStore
}

// WebhookHandlerExport is a test-visible alias that exposes the Store field so
// tests in the handlers_test package can inject a mock WebhookStore.
// Production code should use WebhookHandler directly via RegisterPro.
type WebhookHandlerExport struct {
	Store WebhookStore
}

// Create delegates to the internal WebhookHandler.Create.
func (e *WebhookHandlerExport) Create(c *fiber.Ctx) error {
	h := &WebhookHandler{store: e.Store}
	return h.Create(c)
}

// Delete delegates to the internal WebhookHandler.Delete.
func (e *WebhookHandlerExport) Delete(c *fiber.Ctx) error {
	h := &WebhookHandler{store: e.Store}
	return h.Delete(c)
}

// Create handles POST /v1/webhooks.
// It validates the URL, generates a random per-webhook secret, persists the
// registration, and returns the secret exactly once in the 201 response.
func (h *WebhookHandler) Create(c *fiber.Ctx) error {
	var body struct {
		URL string `json:"url"`
	}
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("request body must be valid JSON with a 'url' field")
	}

	if body.URL == "" {
		return api.NewBadRequest("url is required")
	}
	if _, err := url.ParseRequestURI(body.URL); err != nil {
		return api.NewBadRequest(fmt.Sprintf("invalid url: %s", err.Error()))
	}

	apiKeyHash, _ := c.Locals(middleware.LocalKeyHash).(string)

	// Generate a 32-byte cryptographically random secret.
	rawSecret := make([]byte, 32)
	if _, err := rand.Read(rawSecret); err != nil {
		return api.NewInternalError("failed to generate webhook secret")
	}
	secret := hex.EncodeToString(rawSecret)

	rec, err := h.store.CreateWebhook(c.Context(), apiKeyHash, body.URL, secret)
	if err != nil {
		return api.NewInternalError("failed to register webhook")
	}

	return api.Created(c, createWebhookResponse{
		ID:        rec.ID,
		URL:       rec.URL,
		CreatedAt: rec.CreatedAt,
		Secret:    secret,
	}, api.TrustMeta{})
}

// Delete handles DELETE /v1/webhooks/:id.
// It soft-deletes the webhook identified by the path parameter, scoped to the
// calling key's hash so ownership is enforced.
func (h *WebhookHandler) Delete(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return api.NewBadRequest("webhook id is required")
	}

	apiKeyHash, _ := c.Locals(middleware.LocalKeyHash).(string)

	if err := h.store.DeleteWebhook(c.Context(), id, apiKeyHash); err != nil {
		if err == ErrWebhookNotFound {
			return api.NewNotFound("webhook not found or not owned by this API key")
		}
		return api.NewInternalError("failed to delete webhook")
	}

	return c.SendStatus(fiber.StatusNoContent)
}
