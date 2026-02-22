package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	sdkerrors "github.com/unkeyed/unkey-go/models/sdkerrors"

	"llm-pricing-api/internal/billing"
)

// billingRevokeKeyPayload mirrors handlers.BillingRevokeKeyPayload.
// Duplicated here to avoid an import cycle between the worker and handlers packages.
type billingRevokeKeyPayload struct {
	UnkeyKeyID       string `json:"unkey_key_id"`
	LSSubscriptionID string `json:"ls_subscription_id"`
}

// BillingRevokeKeyStore abstracts DB access for BillingRevokeKeyHandler,
// keeping the handler testable without a live database.
type BillingRevokeKeyStore interface {
	// GetSubscriptionKeyID returns the Unkey key ID for the given subscription.
	GetSubscriptionKeyID(ctx context.Context, lsSubID string) (string, error)
	// ExpireSubscription sets status = 'expired' for the given subscription.
	ExpireSubscription(ctx context.Context, lsSubID string) error
}

// pgxBillingRevokeKeyStore is the PostgreSQL-backed implementation.
type pgxBillingRevokeKeyStore struct{ db *pgxpool.Pool }

// NewBillingRevokeKeyStore returns a BillingRevokeKeyStore backed by the given pool.
func NewBillingRevokeKeyStore(db *pgxpool.Pool) BillingRevokeKeyStore {
	return &pgxBillingRevokeKeyStore{db: db}
}

func (s *pgxBillingRevokeKeyStore) GetSubscriptionKeyID(ctx context.Context, lsSubID string) (string, error) {
	var id string
	if err := s.db.QueryRow(ctx,
		`SELECT unkey_key_id FROM billing_subscriptions WHERE ls_subscription_id = $1`,
		lsSubID,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No subscription row — retrying will not help; skip directly to dead-letter.
			return "", fmt.Errorf("get subscription key id: subscription not found %q: %w",
				lsSubID, asynq.SkipRetry)
		}
		return "", fmt.Errorf("get subscription key id: %w", err)
	}
	return id, nil
}

func (s *pgxBillingRevokeKeyStore) ExpireSubscription(ctx context.Context, lsSubID string) error {
	ct, err := s.db.Exec(ctx, `
		UPDATE billing_subscriptions
		SET status = 'expired', updated_at = NOW()
		WHERE ls_subscription_id = $1
	`, lsSubID)
	if err != nil {
		return fmt.Errorf("expire subscription: %w", err)
	}
	if ct.RowsAffected() == 0 {
		// No matching row — the subscription was never recorded or was already deleted.
		// Skip retries: the row will not appear on subsequent attempts.
		return fmt.Errorf("expire subscription: no row found for %q: %w", lsSubID, asynq.SkipRetry)
	}
	return nil
}

// BillingRevokeKeyHandler handles the "billing:revoke-key" asynq task.
// It revokes the Unkey API key for a cancelled/expired subscription and
// marks the subscription as expired in the database.
type BillingRevokeKeyHandler struct {
	store BillingRevokeKeyStore
	keys  billing.KeyManager
	log   zerolog.Logger
}

// NewBillingRevokeKeyHandler creates a BillingRevokeKeyHandler.
func NewBillingRevokeKeyHandler(db *pgxpool.Pool, keys billing.KeyManager, log zerolog.Logger) *BillingRevokeKeyHandler {
	return &BillingRevokeKeyHandler{store: NewBillingRevokeKeyStore(db), keys: keys, log: log}
}

// Handle executes the billing:revoke-key task.
func (h *BillingRevokeKeyHandler) Handle(ctx context.Context, t *asynq.Task) error {
	var p billingRevokeKeyPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("billing:revoke-key: unmarshal payload: %w", err)
	}

	if p.LSSubscriptionID == "" {
		return fmt.Errorf("billing:revoke-key: ls_subscription_id is required")
	}

	// Look up the subscription to get the unkey_key_id if not provided in payload.
	unkeyKeyID := p.UnkeyKeyID
	if unkeyKeyID == "" {
		var err error
		unkeyKeyID, err = h.store.GetSubscriptionKeyID(ctx, p.LSSubscriptionID)
		if err != nil {
			return fmt.Errorf("billing:revoke-key: lookup subscription: %w", err)
		}
	}

	if err := h.keys.RevokeKey(ctx, unkeyKeyID); err != nil {
		// If the key is already deleted (e.g. a prior retry succeeded at RevokeKey
		// but then failed at ExpireSubscription), the security goal is already met —
		// treat it as a no-op and continue so the DB row is correctly expired.
		var notFound *sdkerrors.ErrNotFound
		if !errors.As(err, &notFound) {
			return fmt.Errorf("billing:revoke-key: revoke key %q: %w", unkeyKeyID, err)
		}
		h.log.Warn().
			Str("unkey_key_id", unkeyKeyID).
			Str("ls_sub_id", p.LSSubscriptionID).
			Msg("billing:revoke-key: key already revoked; proceeding to expire subscription")
	}

	if err := h.store.ExpireSubscription(ctx, p.LSSubscriptionID); err != nil {
		return fmt.Errorf("billing:revoke-key: update subscription status: %w", err)
	}

	h.log.Info().
		Str("ls_sub_id", p.LSSubscriptionID).
		Str("unkey_key_id", unkeyKeyID).
		Msg("billing:revoke-key: key revoked and subscription expired")
	return nil
}
