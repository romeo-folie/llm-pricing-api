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
	CreateKey(ctx context.Context, email, tier string) (keyID string, keyValue string, err error)
	UpdateKeyTier(ctx context.Context, unkeyKeyID, tier string) error
	RevokeKey(ctx context.Context, unkeyKeyID string) error
}

// Emailer is the transactional email interface.
// Note: ctx is accepted for interface consistency and future SDK compatibility;
// the underlying Resend SDK (v1.7.0) does not support context propagation internally.
type Emailer interface {
	SendKeyDelivery(ctx context.Context, email, tier, keyValue string) error
	SendPlanChange(ctx context.Context, email, oldTier, newTier string, renewsAt time.Time) error
	SendCancellation(ctx context.Context, email string, renewsAt time.Time) error
}

// Config holds credentials for all three external services.
type Config struct {
	LSAPIKey        string
	LSStoreID       string
	UnkeyRootKey    string
	UnkeyAPIID      string
	ResendAPIKey    string
	ResendFromEmail string
	// DocsURL is linked in the API key delivery email. Defaults to https://llmrates.com/docs.
	DocsURL string
}

// Service wires the Lemon Squeezy, Unkey, and Resend clients together.
// Handlers and jobs access LS, Keys, and Email directly.
type Service struct {
	LS    SubscriptionManager
	Keys  KeyManager
	Email Emailer
}

// NewService constructs a Service from the given Config.
// Returns an error if required credentials are missing or if any client fails
// to initialise (currently only the email client can fail, due to embedded
// template parsing).
func NewService(cfg Config) (*Service, error) {
	if cfg.UnkeyRootKey == "" {
		return nil, fmt.Errorf("billing: UnkeyRootKey is required")
	}
	if cfg.UnkeyAPIID == "" {
		return nil, fmt.Errorf("billing: UnkeyAPIID is required")
	}
	if cfg.LSAPIKey == "" {
		return nil, fmt.Errorf("billing: LSAPIKey is required")
	}

	docsURL := cfg.DocsURL
	if docsURL == "" {
		docsURL = "https://llmrates.com/docs"
	}

	emailClient, err := NewEmailClient(cfg.ResendAPIKey, cfg.ResendFromEmail, docsURL)
	if err != nil {
		return nil, fmt.Errorf("billing: init email client: %w", err)
	}
	return &Service{
		LS:    NewLemonSqueezyClient(cfg.LSAPIKey, cfg.LSStoreID),
		Keys:  NewUnkeyClient(cfg.UnkeyRootKey, cfg.UnkeyAPIID),
		Email: emailClient,
	}, nil
}
