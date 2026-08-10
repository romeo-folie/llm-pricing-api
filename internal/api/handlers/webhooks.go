// Package handlers contains Fiber HTTP handler implementations for the
// LLM pricing REST API endpoints.
package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

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
	// secret is stored encrypted in the DB; the reconciler decrypts it before use.
	// Returns ErrWebhookLimitReached when the key already holds maxWebhooksPerKey
	// active webhooks.
	CreateWebhook(ctx context.Context, apiKeyHash, webhookURL, secret string) (WebhookRecord, error)

	// DeleteWebhook soft-deletes a webhook by ID, scoped to the given apiKeyHash
	// so that a key cannot delete another key's webhook.
	// Returns ErrWebhookNotFound if no matching active row exists.
	DeleteWebhook(ctx context.Context, id, apiKeyHash string) error
}

// ErrWebhookNotFound is returned by DeleteWebhook when no matching row is found.
var ErrWebhookNotFound = fmt.Errorf("webhook not found")

// ErrWebhookLimitReached is returned by CreateWebhook when the calling key
// already holds maxWebhooksPerKey active webhooks.
var ErrWebhookLimitReached = fmt.Errorf("webhook limit reached")

// maxWebhooksPerKey caps how many active (non-deleted) webhooks a single API
// key may hold. Registration is no longer tier-gated, so this is what bounds
// delivery fan-out per key: every confirmed price change enqueues one delivery
// job per active webhook.
const maxWebhooksPerKey = 5

// pgxWebhookStore is the PostgreSQL-backed implementation of WebhookStore.
type pgxWebhookStore struct {
	db *pgxpool.Pool
}

// NewWebhookStore returns a WebhookStore backed by the provided connection pool.
func NewWebhookStore(db *pgxpool.Pool) WebhookStore {
	return &pgxWebhookStore{db: db}
}

func (s *pgxWebhookStore) CreateWebhook(ctx context.Context, apiKeyHash, webhookURL, secret string) (WebhookRecord, error) {
	// Serialize registrations for the same API key before checking the cap.
	// Under PostgreSQL's default READ COMMITTED isolation, putting count(*) in
	// the INSERT alone is not sufficient: concurrent statements can share the
	// same pre-insert view and all pass the limit. The separate lock statement
	// also ensures the following statement receives a fresh snapshot after any
	// preceding registration commits.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return WebhookRecord{}, fmt.Errorf("create webhook: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, apiKeyHash); err != nil {
		return WebhookRecord{}, fmt.Errorf("create webhook: lock API key: %w", err)
	}

	var rec WebhookRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO webhooks (api_key_hash, url, secret)
		SELECT $1, $2, $3
		WHERE (
			SELECT count(*) FROM webhooks
			WHERE api_key_hash = $1 AND deleted_at IS NULL
		) < $4
		RETURNING id, url, created_at
	`, apiKeyHash, webhookURL, secret, maxWebhooksPerKey).Scan(&rec.ID, &rec.URL, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return WebhookRecord{}, ErrWebhookLimitReached
	}
	if err != nil {
		return WebhookRecord{}, fmt.Errorf("create webhook: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return WebhookRecord{}, fmt.Errorf("create webhook: commit: %w", err)
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

// encryptSecret encrypts plaintext using AES-256-GCM with the provided 32-byte key.
// Returns a hex-encoded string of nonce || ciphertext.
func encryptSecret(plaintext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return hex.EncodeToString(ciphertext), nil
}

// decryptSecret decrypts a hex-encoded nonce || ciphertext using AES-256-GCM.
func decryptSecret(encHex string, key []byte) ([]byte, error) {
	data, err := hex.DecodeString(encHex)
	if err != nil {
		return nil, fmt.Errorf("decode hex ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// resolveWebhookKey returns a 32-byte AES key derived from hexKey.
// If hexKey is empty, a random ephemeral key is generated and a warning is logged.
// The caller must use the same key for encrypt and decrypt — ephemeral keys do
// not survive process restarts.
func resolveWebhookKey(hexKey string, log zerolog.Logger) ([]byte, error) {
	if hexKey == "" {
		log.Warn().Msg("WEBHOOK_SECRET_KEY is not set; using ephemeral key — webhook secrets will not survive restarts")
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate ephemeral webhook key: %w", err)
		}
		return key, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("WEBHOOK_SECRET_KEY must be a 64-char hex-encoded 32-byte key")
	}
	return key, nil
}

// isWebhookURLSafe resolves the hostname of webhookURL and returns an error if
// any resolved address is a loopback, private, link-local, or unspecified IP.
// This prevents SSRF attacks via DNS rebinding or direct private-IP targets.
// Fails closed: if DNS resolution fails entirely, the URL is rejected.
func isWebhookURLSafe(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("missing hostname")
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("could not resolve hostname %q", host)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("webhook URL resolves to a private or loopback address")
		}
	}
	return nil
}

// WebhookHandler handles POST /v1/webhooks and DELETE /v1/webhooks/:id.
type WebhookHandler struct {
	store        WebhookStore
	secretKey    []byte                              // 32-byte AES-256-GCM key for encrypting webhook secrets at rest
	urlValidator func(context.Context, string) error // nil → uses isWebhookURLSafe; overridden in tests
}

// WebhookHandlerExport is a test-visible alias that exposes the Store field so
// tests in the handlers_test package can inject a mock WebhookStore.
// Production code should use WebhookHandler directly via RegisterPro.
//
// WARNING: This shim does not set a secretKey, so webhook secrets are stored
// and used as plaintext. It is intended solely for unit tests that do not
// need end-to-end encryption. Never use in production code paths.
//
// Set UrlValidator to a custom func to override SSRF DNS checks in tests.
// A nil UrlValidator uses the real isWebhookURLSafe (DNS resolution required).
type WebhookHandlerExport struct {
	Store        WebhookStore
	UrlValidator func(context.Context, string) error
}

// Create delegates to the internal WebhookHandler.Create.
func (e *WebhookHandlerExport) Create(c *fiber.Ctx) error {
	h := &WebhookHandler{store: e.Store, urlValidator: e.UrlValidator}
	return h.Create(c)
}

// Delete delegates to the internal WebhookHandler.Delete.
func (e *WebhookHandlerExport) Delete(c *fiber.Ctx) error {
	h := &WebhookHandler{store: e.Store, urlValidator: e.UrlValidator}
	return h.Delete(c)
}

// Create handles POST /v1/webhooks.
// It validates the URL, generates a random per-webhook secret, encrypts it
// with AES-256-GCM before persisting, and returns the plaintext secret exactly
// once in the 201 response.
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
	parsed, err := url.ParseRequestURI(body.URL)
	if err != nil {
		return api.NewBadRequest(fmt.Sprintf("invalid url: %s", err.Error()))
	}
	if parsed.Scheme != "https" {
		return api.NewBadRequest("url scheme must be https")
	}
	validator := h.urlValidator
	if validator == nil {
		validator = isWebhookURLSafe
	}
	if err := validator(c.Context(), body.URL); err != nil {
		return api.NewBadRequest(fmt.Sprintf("url rejected: %s", err.Error()))
	}

	apiKeyHash, _ := c.Locals(middleware.LocalKeyHash).(string)

	// Generate a 32-byte cryptographically random secret.
	rawSecret := make([]byte, 32)
	if _, err := rand.Read(rawSecret); err != nil {
		return api.NewInternalError("failed to generate webhook secret")
	}
	plainSecret := hex.EncodeToString(rawSecret)

	// Encrypt the secret before storing. If no key is configured on the handler
	// (e.g. test paths using WebhookHandlerExport), store plaintext as before.
	secretToStore := plainSecret
	if len(h.secretKey) == 32 {
		encSecret, err := encryptSecret([]byte(plainSecret), h.secretKey)
		if err != nil {
			return api.NewInternalError("failed to encrypt webhook secret")
		}
		secretToStore = encSecret
	}

	rec, err := h.store.CreateWebhook(c.Context(), apiKeyHash, body.URL, secretToStore)
	if errors.Is(err, ErrWebhookLimitReached) {
		pd := api.NewConflict(fmt.Sprintf(
			"this API key already has the maximum of %d active webhooks; delete one before registering another",
			maxWebhooksPerKey))
		pd.Extensions = map[string]any{"max_webhooks": maxWebhooksPerKey}
		return pd
	}
	if err != nil {
		return api.NewInternalError("failed to register webhook")
	}

	return api.Created(c, createWebhookResponse{
		ID:        rec.ID,
		URL:       rec.URL,
		CreatedAt: rec.CreatedAt,
		Secret:    plainSecret,
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

// DecryptWebhookSecret decrypts an encrypted webhook secret using the provided
// hex-encoded 32-byte key. It is exported so the reconciler can decrypt secrets
// read from the database before passing them to delivery workers.
func DecryptWebhookSecret(encSecret, hexKey string) (string, error) {
	if hexKey == "" {
		// No key configured — secret stored as plaintext (ephemeral-key startup path).
		return encSecret, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid WEBHOOK_SECRET_KEY: must be 64-char hex-encoded 32-byte key")
	}
	plain, err := decryptSecret(encSecret, key)
	if err != nil {
		// Fall back to treating the value as plaintext (handles rows written before
		// encryption was enabled).
		return encSecret, nil
	}
	return string(plain), nil
}
