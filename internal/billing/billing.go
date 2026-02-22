package billing

import (
	"context"
	"fmt"
	"time"
)

// SubscriptionManager is the Lemon Squeezy client interface.
type SubscriptionManager interface {
	GetSubscription(ctx context.Context, lsSubscriptionID string) (*Subscription, error)
	GetCustomerPortalURL(ctx context.Context, lsSubscriptionID string) (string, error)
}

// KeyManager is the Unkey key lifecycle interface.
type KeyManager interface {
	CreateKey(email, tier string) (keyID string, keyValue string, err error)
	UpdateKeyTier(unkeyKeyID, tier string) error
	RevokeKey(unkeyKeyID string) error
}

// Emailer is the transactional email interface.
type Emailer interface {
	SendKeyDelivery(email, tier, keyValue string) error
	SendPlanChange(email, oldTier, newTier string, renewsAt time.Time) error
	SendCancellation(email string, renewsAt time.Time) error
}

// Config holds credentials for all three external services.
type Config struct {
	LSAPIKey        string
	LSStoreID       string
	UnkeyRootKey    string
	UnkeyAPIID      string
	ResendAPIKey    string
	ResendFromEmail string
}

// Service wires the Lemon Squeezy, Unkey, and Resend clients together.
// Handlers and jobs access LS, Keys, and Email directly.
type Service struct {
	LS    SubscriptionManager
	Keys  KeyManager
	Email Emailer
}

// NewService constructs a Service from the given Config.
// Returns an error if any client fails to initialise (currently only the email
// client can fail, due to embedded template parsing).
func NewService(cfg Config) (*Service, error) {
	emailClient, err := NewEmailClient(cfg.ResendAPIKey, cfg.ResendFromEmail)
	if err != nil {
		return nil, fmt.Errorf("billing: init email client: %w", err)
	}
	return &Service{
		LS:    NewLemonSqueezyClient(cfg.LSAPIKey, cfg.LSStoreID),
		Keys:  NewUnkeyClient(cfg.UnkeyRootKey, cfg.UnkeyAPIID),
		Email: emailClient,
	}, nil
}
