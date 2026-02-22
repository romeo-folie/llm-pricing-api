package handlers

import (
	"context"
	"regexp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/billing"
)

// TypeBillingRevokeKey re-exports the canonical constant from internal/billing.
// The single definition in internal/billing eliminates any risk of the handler
// and worker sides drifting to different string values.
const TypeBillingRevokeKey = billing.TaskBillingRevokeKey

// billingQueue is the asynq queue used for all billing tasks.
// Must match the worker configuration.
const billingQueue = "default"

// dispatchTimeout caps the lifetime of each background billing goroutine.
// External API calls (Unkey, Resend) and DB queries must complete within
// this window or they are cancelled and the error is logged.
const dispatchTimeout = 30 * time.Second

// BillingRevokeKeyPayload is the JSON payload stored in the asynq task.
type BillingRevokeKeyPayload struct {
	UnkeyKeyID       string `json:"unkey_key_id"`
	LSSubscriptionID string `json:"ls_subscription_id"`
}

// BillingSubscription is a DB row from billing_subscriptions.
type BillingSubscription struct {
	UnkeyKeyID   string
	Email        string
	Tier         string
	RevokeJobID  *string
	CancelledAt  *time.Time
	Status       string
}

// LemonSqueezyStore abstracts the DB operations required by LemonSqueezyHandler.
type LemonSqueezyStore interface {
	// TryInsertWebhookEvent atomically records a processed event for idempotency
	// using INSERT ... ON CONFLICT DO NOTHING. Returns (true, nil) when the row
	// was newly inserted, (false, nil) when the event already exists (duplicate
	// delivery — safe to ignore), and (false, err) on any other DB error.
	TryInsertWebhookEvent(ctx context.Context, eventID, eventType string) (inserted bool, err error)
	// CreateSubscription inserts a new billing_subscriptions row.
	CreateSubscription(ctx context.Context, lsSubID, email, tier, unkeyKeyID string) error
	// GetSubscription returns the subscription row for lsSubID.
	// Returns ErrBillingSubscriptionNotFound when no row matches.
	GetSubscription(ctx context.Context, lsSubID string) (BillingSubscription, error)
	// UpdateSubscriptionTier updates the tier column for lsSubID.
	UpdateSubscriptionTier(ctx context.Context, lsSubID, tier string) error
	// UpdateSubscriptionKey updates the unkey_key_id column for lsSubID.
	// Used when a new key is issued on subscription resume after expiry.
	UpdateSubscriptionKey(ctx context.Context, lsSubID, unkeyKeyID string) error
	// CancelSubscription sets cancelled_at = NOW() and revoke_job_id for lsSubID.
	CancelSubscription(ctx context.Context, lsSubID, revokeJobID string) error
	// ExpireSubscription sets status = 'expired' for lsSubID.
	ExpireSubscription(ctx context.Context, lsSubID string) error
	// ResumeSubscription clears cancelled_at and revoke_job_id, sets status = 'active'.
	ResumeSubscription(ctx context.Context, lsSubID string) error
}

// ErrBillingSubscriptionNotFound is returned by LemonSqueezyStore when no
// billing_subscriptions row matches the requested ls_subscription_id.
var ErrBillingSubscriptionNotFound = fmt.Errorf("billing subscription not found")

// pgxLemonSqueezyStore is the PostgreSQL-backed implementation.
type pgxLemonSqueezyStore struct {
	db *pgxpool.Pool
}

// NewLemonSqueezyStore returns a LemonSqueezyStore backed by the given pool.
func NewLemonSqueezyStore(db *pgxpool.Pool) LemonSqueezyStore {
	return &pgxLemonSqueezyStore{db: db}
}

func (s *pgxLemonSqueezyStore) TryInsertWebhookEvent(ctx context.Context, eventID, eventType string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`INSERT INTO webhook_events (event_id, event_type) VALUES ($1, $2) ON CONFLICT (event_id) DO NOTHING`,
		eventID, eventType,
	)
	if err != nil {
		return false, fmt.Errorf("try insert webhook event: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *pgxLemonSqueezyStore) CreateSubscription(ctx context.Context, lsSubID, email, tier, unkeyKeyID string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO billing_subscriptions
			(ls_subscription_id, ls_customer_email, tier, unkey_key_id, status)
		VALUES ($1, $2, $3, $4, 'active')
	`, lsSubID, email, tier, unkeyKeyID)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

func (s *pgxLemonSqueezyStore) GetSubscription(ctx context.Context, lsSubID string) (BillingSubscription, error) {
	var sub BillingSubscription
	err := s.db.QueryRow(ctx, `
		SELECT unkey_key_id, ls_customer_email, tier, revoke_job_id, cancelled_at, status
		FROM billing_subscriptions
		WHERE ls_subscription_id = $1
	`, lsSubID).Scan(
		&sub.UnkeyKeyID, &sub.Email, &sub.Tier,
		&sub.RevokeJobID, &sub.CancelledAt, &sub.Status,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return BillingSubscription{}, ErrBillingSubscriptionNotFound
		}
		return BillingSubscription{}, fmt.Errorf("get subscription: %w", err)
	}
	return sub, nil
}

func (s *pgxLemonSqueezyStore) UpdateSubscriptionTier(ctx context.Context, lsSubID, tier string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE billing_subscriptions
		SET tier = $1, updated_at = NOW()
		WHERE ls_subscription_id = $2
	`, tier, lsSubID)
	if err != nil {
		return fmt.Errorf("update subscription tier: %w", err)
	}
	return nil
}

func (s *pgxLemonSqueezyStore) UpdateSubscriptionKey(ctx context.Context, lsSubID, unkeyKeyID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE billing_subscriptions
		SET unkey_key_id = $1, updated_at = NOW()
		WHERE ls_subscription_id = $2
	`, unkeyKeyID, lsSubID)
	if err != nil {
		return fmt.Errorf("update subscription key: %w", err)
	}
	return nil
}

func (s *pgxLemonSqueezyStore) CancelSubscription(ctx context.Context, lsSubID, revokeJobID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE billing_subscriptions
		SET cancelled_at = NOW(), revoke_job_id = $1, status = 'cancelled', updated_at = NOW()
		WHERE ls_subscription_id = $2
	`, revokeJobID, lsSubID)
	if err != nil {
		return fmt.Errorf("cancel subscription: %w", err)
	}
	return nil
}

func (s *pgxLemonSqueezyStore) ExpireSubscription(ctx context.Context, lsSubID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE billing_subscriptions
		SET status = 'expired', updated_at = NOW()
		WHERE ls_subscription_id = $1
	`, lsSubID)
	if err != nil {
		return fmt.Errorf("expire subscription: %w", err)
	}
	return nil
}

func (s *pgxLemonSqueezyStore) ResumeSubscription(ctx context.Context, lsSubID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE billing_subscriptions
		SET cancelled_at = NULL, revoke_job_id = NULL, status = 'active', updated_at = NOW()
		WHERE ls_subscription_id = $1
	`, lsSubID)
	if err != nil {
		return fmt.Errorf("resume subscription: %w", err)
	}
	return nil
}

// lsWebhookPayload is the minimal Lemon Squeezy webhook event structure.
type lsWebhookPayload struct {
	Meta struct {
		EventName string `json:"event_name"`
		WebhookID string `json:"webhook_id"`
	} `json:"meta"`
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			UserEmail string     `json:"user_email"`
			VariantID int        `json:"variant_id"`
			Status    string     `json:"status"`
			RenewsAt  *time.Time `json:"renews_at"`
			EndsAt    *time.Time `json:"ends_at"`
		} `json:"attributes"`
	} `json:"data"`
}

// LemonSqueezyHandler handles POST /webhooks/lemon-squeezy.
// It verifies HMAC-SHA256 signatures, enforces idempotency, and dispatches
// subscription lifecycle events to internal/billing in a background goroutine.
type LemonSqueezyHandler struct {
	store         LemonSqueezyStore
	keys          billing.KeyManager
	email         billing.Emailer
	enqueueTask   func(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	deleteTask    func(queue, taskID string) error
	signingSecret string
	variantDev    string
	variantPro    string
	log           zerolog.Logger
}

// LemonSqueezyHandlerConfig holds all constructor parameters for LemonSqueezyHandler.
type LemonSqueezyHandlerConfig struct {
	Store         LemonSqueezyStore
	Keys          billing.KeyManager
	Email         billing.Emailer
	EnqueueTask   func(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	DeleteTask    func(queue, taskID string) error
	SigningSecret string
	VariantDev    string
	VariantPro    string
	Log           zerolog.Logger
}

// NewLemonSqueezyHandler constructs a LemonSqueezyHandler.
func NewLemonSqueezyHandler(cfg LemonSqueezyHandlerConfig) *LemonSqueezyHandler {
	return &LemonSqueezyHandler{
		store:         cfg.Store,
		keys:          cfg.Keys,
		email:         cfg.Email,
		enqueueTask:   cfg.EnqueueTask,
		deleteTask:    cfg.DeleteTask,
		signingSecret: cfg.SigningSecret,
		variantDev:    cfg.VariantDev,
		variantPro:    cfg.VariantPro,
		log:           cfg.Log,
	}
}

// Handle is the Fiber handler for POST /webhooks/lemon-squeezy.
func (h *LemonSqueezyHandler) Handle(c *fiber.Ctx) error {
	// Read raw body BEFORE any JSON parsing — Fiber consumes body on parse.
	rawBody := c.Body()

	// 1. Verify HMAC-SHA256 signature.
	sig := c.Get("X-Signature")
	if !h.verifySignature(rawBody, sig) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "invalid signature"})
	}

	// 2. Parse payload.
	var payload lsWebhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
	}

	eventID := payload.Meta.WebhookID
	eventType := payload.Meta.EventName

	// 3. Atomic idempotency: INSERT ON CONFLICT DO NOTHING.
	// A single DB round-trip enforced by the unique constraint on event_id.
	// Returns false when the event was already processed (duplicate delivery).
	inserted, err := h.store.TryInsertWebhookEvent(c.Context(), eventID, eventType)
	if err != nil {
		h.log.Error().Err(err).Str("event_id", eventID).Msg("lssqueezy: idempotency insert failed")
		return c.SendStatus(fiber.StatusOK)
	}
	if !inserted {
		// Duplicate delivery — already processed; Lemon Squeezy expects 200.
		return c.SendStatus(fiber.StatusOK)
	}

	// 5. Dispatch to billing in a background goroutine.
	// Take copies of everything the goroutine needs so the Fiber context can be released.
	lsSubID := payload.Data.ID
	email := payload.Data.Attributes.UserEmail
	variantIDStr := strconv.Itoa(payload.Data.Attributes.VariantID)
	renewsAt := payload.Data.Attributes.RenewsAt
	endsAt := payload.Data.Attributes.EndsAt

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), dispatchTimeout)
		defer cancel()
		if dispatchErr := h.dispatch(ctx, eventType, lsSubID, email, variantIDStr, renewsAt, endsAt); dispatchErr != nil {
			h.log.Error().Err(dispatchErr).
				Str("event_type", eventType).
				Str("ls_sub_id", lsSubID).
				Msg("lssqueezy: dispatch failed")
		}
	}()

	return c.SendStatus(fiber.StatusOK)
}

// verifySignature validates the HMAC-SHA256 signature of the raw body using the
// configured signing secret. Returns false on any mismatch, missing signature,
// or unconfigured secret. Fails closed: an empty signing secret always returns false.
func (h *LemonSqueezyHandler) verifySignature(body []byte, sig string) bool {
	if h.signingSecret == "" {
		// Fail closed: reject all requests when no signing secret is configured.
		return false
	}
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// tierFromVariant maps a Lemon Squeezy variant ID string to an internal tier.
// Returns "" if the variant ID is not recognised.
func (h *LemonSqueezyHandler) tierFromVariant(variantIDStr string) string {
	switch variantIDStr {
	case h.variantDev:
		return "developer"
	case h.variantPro:
		return "pro"
	default:
		return ""
	}
}

// dispatch runs the appropriate billing action for the given event type.
// It is called in a background goroutine and must not panic.
func (h *LemonSqueezyHandler) dispatch(
	ctx context.Context,
	eventType, lsSubID, email, variantIDStr string,
	renewsAt, endsAt *time.Time,
) error {
	switch eventType {
	case "subscription_created":
		return h.handleCreated(ctx, lsSubID, email, variantIDStr)

	case "subscription_updated":
		return h.handleUpdated(ctx, lsSubID, variantIDStr, renewsAt)

	case "subscription_cancelled":
		return h.handleCancelled(ctx, lsSubID, endsAt)

	case "subscription_expired":
		return h.handleExpired(ctx, lsSubID)

	case "subscription_resumed":
		return h.handleResumed(ctx, lsSubID)

	default:
		// Unknown event types are logged and ignored (subscription_paused, payment_*, etc.)
		return nil
	}
}

func (h *LemonSqueezyHandler) handleCreated(ctx context.Context, lsSubID, email, variantIDStr string) error {
	tier := h.tierFromVariant(variantIDStr)
	if tier == "" {
		return fmt.Errorf("subscription_created: unknown variant %q", variantIDStr)
	}

	keyID, keyValue, err := h.keys.CreateKey(ctx, email, tier)
	if err != nil {
		return fmt.Errorf("subscription_created: create key: %w", err)
	}

	if err := h.store.CreateSubscription(ctx, lsSubID, email, tier, keyID); err != nil {
		// Best-effort cleanup: the Unkey key exists but the subscription row could
		// not be persisted. Revoke the orphaned key so the user is not charged for
		// access they cannot use. Log on failure — ops must intervene manually.
		if revokeErr := h.keys.RevokeKey(ctx, keyID); revokeErr != nil {
			h.log.Error().Err(revokeErr).
				Str("unkey_key_id", keyID).
				Str("ls_sub_id", lsSubID).
				Msg("subscription_created: failed to revoke orphaned key after store failure — manual cleanup required")
		}
		return fmt.Errorf("subscription_created: store subscription: %w", err)
	}

	if err := h.email.SendKeyDelivery(ctx, email, tier, keyValue); err != nil {
		// Non-fatal: key is created and stored; email failure is logged.
		return fmt.Errorf("subscription_created: send key delivery email: %w", err)
	}
	return nil
}

func (h *LemonSqueezyHandler) handleUpdated(ctx context.Context, lsSubID, variantIDStr string, renewsAt *time.Time) error {
	newTier := h.tierFromVariant(variantIDStr)
	if newTier == "" {
		return fmt.Errorf("subscription_updated: unknown variant %q", variantIDStr)
	}

	sub, err := h.store.GetSubscription(ctx, lsSubID)
	if err != nil {
		return fmt.Errorf("subscription_updated: get subscription: %w", err)
	}

	// Lemon Squeezy sends subscription_updated for many reasons (billing address,
	// payment method, etc.) — only act when the tier actually changes.
	if sub.Tier == newTier {
		return nil
	}

	if err := h.keys.UpdateKeyTier(ctx, sub.UnkeyKeyID, newTier); err != nil {
		return fmt.Errorf("subscription_updated: update key tier: %w", err)
	}

	if err := h.store.UpdateSubscriptionTier(ctx, lsSubID, newTier); err != nil {
		return fmt.Errorf("subscription_updated: update subscription tier: %w", err)
	}

	var renews time.Time
	if renewsAt != nil {
		renews = *renewsAt
	}
	if err := h.email.SendPlanChange(ctx, sub.Email, sub.Tier, newTier, renews); err != nil {
		return fmt.Errorf("subscription_updated: send plan change email: %w", err)
	}
	return nil
}

func (h *LemonSqueezyHandler) handleCancelled(ctx context.Context, lsSubID string, endsAt *time.Time) error {
	if h.enqueueTask == nil {
		return fmt.Errorf("subscription_cancelled: asynq client not configured")
	}

	sub, err := h.store.GetSubscription(ctx, lsSubID)
	if err != nil {
		return fmt.Errorf("subscription_cancelled: get subscription: %w", err)
	}

	payload, err := json.Marshal(BillingRevokeKeyPayload{UnkeyKeyID: sub.UnkeyKeyID, LSSubscriptionID: lsSubID})
	if err != nil {
		return fmt.Errorf("subscription_cancelled: marshal task payload: %w", err)
	}

	task := asynq.NewTask(TypeBillingRevokeKey, payload)
	// Always specify the queue explicitly so enqueue and delete reference the
	// same billingQueue constant — avoids silent misrouting if queue names change.
	opts := []asynq.Option{asynq.Queue(billingQueue), asynq.MaxRetry(3), asynq.Timeout(30 * time.Second)}
	if endsAt != nil && endsAt.After(time.Now()) {
		opts = append(opts, asynq.ProcessAt(*endsAt))
	}

	info, err := h.enqueueTask(task, opts...)
	if err != nil {
		return fmt.Errorf("subscription_cancelled: enqueue revoke-key task: %w", err)
	}

	if err := h.store.CancelSubscription(ctx, lsSubID, info.ID); err != nil {
		return fmt.Errorf("subscription_cancelled: update subscription: %w", err)
	}
	return nil
}

func (h *LemonSqueezyHandler) handleExpired(ctx context.Context, lsSubID string) error {
	sub, err := h.store.GetSubscription(ctx, lsSubID)
	if err != nil {
		return fmt.Errorf("subscription_expired: get subscription: %w", err)
	}

	if err := h.keys.RevokeKey(ctx, sub.UnkeyKeyID); err != nil {
		return fmt.Errorf("subscription_expired: revoke key: %w", err)
	}

	if err := h.store.ExpireSubscription(ctx, lsSubID); err != nil {
		return fmt.Errorf("subscription_expired: update subscription: %w", err)
	}
	return nil
}

func (h *LemonSqueezyHandler) handleResumed(ctx context.Context, lsSubID string) error {
	sub, err := h.store.GetSubscription(ctx, lsSubID)
	if err != nil {
		return fmt.Errorf("subscription_resumed: get subscription: %w", err)
	}

	// Cancel the pending revoke-key job if one was recorded.
	if sub.RevokeJobID != nil && *sub.RevokeJobID != "" && h.deleteTask != nil {
		if err := h.deleteTask(billingQueue, *sub.RevokeJobID); err != nil {
			// Non-fatal: log and continue. The task may have already fired or been deleted.
			h.log.Warn().Err(err).
				Str("task_id", *sub.RevokeJobID).
				Msg("lssqueezy: could not cancel revoke-key job")
		}
	}

	// If the subscription is already 'expired' the revoke-key task ran before
	// this resume event arrived. The Unkey key is dead — create a new one and
	// deliver it to the user so their access is restored.
	if sub.Status == "expired" {
		newKeyID, newKeyValue, err := h.keys.CreateKey(ctx, sub.Email, sub.Tier)
		if err != nil {
			return fmt.Errorf("subscription_resumed: create replacement key: %w", err)
		}
		if err := h.store.UpdateSubscriptionKey(ctx, lsSubID, newKeyID); err != nil {
			// Compensate: revoke the orphaned key we just created.
			if revokeErr := h.keys.RevokeKey(ctx, newKeyID); revokeErr != nil {
				h.log.Error().Err(revokeErr).Str("unkey_key_id", newKeyID).
					Msg("subscription_resumed: failed to revoke orphaned replacement key — manual cleanup required")
			}
			return fmt.Errorf("subscription_resumed: update subscription key: %w", err)
		}
		if err := h.email.SendKeyDelivery(ctx, sub.Email, sub.Tier, newKeyValue); err != nil {
			// Non-fatal: key is live; log and continue.
			h.log.Warn().Err(err).
				Str("ls_sub_id", lsSubID).
				Msg("subscription_resumed: failed to send key delivery email after reactivation")
		}
	}

	if err := h.store.ResumeSubscription(ctx, lsSubID); err != nil {
		return fmt.Errorf("subscription_resumed: update subscription: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// FreeSignupHandler
// ---------------------------------------------------------------------------

// emailRe is a simple email format validator.
// It rejects empty local parts, empty domains, and domains without a TLD.
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// FreeSignupHandler handles POST /v1/signup/free.
// It is unauthenticated. It validates email, enforces per-IP rate limiting
// via Redis, creates a Free-tier Unkey key, and delivers it by email.
// The key value is never included in the HTTP response.
type FreeSignupHandler struct {
	keys  billing.KeyManager
	email billing.Emailer
	rdb   *redis.Client
	log   zerolog.Logger
}

// NewFreeSignupHandler constructs a FreeSignupHandler.
func NewFreeSignupHandler(keys billing.KeyManager, email billing.Emailer, rdb *redis.Client, log zerolog.Logger) *FreeSignupHandler {
	return &FreeSignupHandler{
		keys:  keys,
		email: email,
		rdb:   rdb,
		log:   log,
	}
}

// Handle is the Fiber handler for POST /v1/signup/free.
func (h *FreeSignupHandler) Handle(c *fiber.Ctx) error {
	// 1. Parse request body.
	var body struct {
		Email string `json:"email"`
	}
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid request body")
	}

	// 2. Validate email.
	email := body.Email
	if email == "" || !emailRe.MatchString(email) {
		return api.NewBadRequest("invalid email address")
	}

	// 3. Per-IP rate limiting.
	// Use the raw TCP peer address (RemoteIP) rather than c.IP() to prevent
	// X-Forwarded-For spoofing — callers cannot inject a fake IP via headers.
	// The IP is hashed with SHA-256 so raw addresses are never stored in Redis.
	//
	// Invariant: SET NX seeds the counter at 0, so INCR starts at 1.
	// Allowed requests: count ∈ {1,2,3}. Rejected: count > 3 (4th+ request).
	// Changing the seed or the threshold independently will silently break the limit.
	ipHash := sha256.Sum256([]byte(c.Context().RemoteIP().String()))
	rlKey := fmt.Sprintf("signup:free:%x", ipHash)

	// SET key 0 NX EX 86400 — atomically creates the key with TTL on first use.
	// redis.Nil is returned when the key already exists (NX condition not met) — not an error.
	if err := h.rdb.SetArgs(c.Context(), rlKey, 0, redis.SetArgs{
		Mode: "NX",
		TTL:  86400 * time.Second,
	}).Err(); err != nil && err != redis.Nil {
		h.log.Error().Err(err).Msg("free signup: redis SET NX failed")
		return api.NewInternalError("could not provision API key")
	}

	count, err := h.rdb.Incr(c.Context(), rlKey).Result()
	if err != nil {
		h.log.Error().Err(err).Msg("free signup: redis INCR failed")
		return api.NewInternalError("could not provision API key")
	}
	if count > 3 {
		c.Set("Retry-After", "86400")
		return api.NewTooManyRequests("signup rate limit exceeded — try again in 24 hours")
	}

	// 4. Create Free-tier Unkey key.
	_, keyValue, err := h.keys.CreateKey(c.Context(), email, "free")
	if err != nil {
		// Log a truncated hash of the email for debugging without storing PII.
		emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(email)))[:16]
		h.log.Error().Err(err).Str("email_hash", emailHash).Msg("free signup: key creation failed")
		return api.NewInternalError("could not provision API key")
	}

	// 5. Deliver key by email in a background goroutine.
	// keyValue is captured in the closure but is never logged or returned in the HTTP response.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), dispatchTimeout)
		defer cancel()
		if err := h.email.SendKeyDelivery(ctx, email, "free", keyValue); err != nil {
			emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(email)))[:16]
			h.log.Error().Err(err).Str("email_hash", emailHash).Msg("free signup: email delivery failed")
		}
	}()

	// 6. Return a success response that does NOT include the key value.
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "API key sent to your email"})
}
