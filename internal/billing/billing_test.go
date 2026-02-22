package billing_test

import (
	"testing"

	"llm-pricing-api/internal/billing"
)

// Compile-time interface satisfaction checks. These blank-identifier
// assignments verify that the concrete types returned by NewService implement
// their respective interfaces. If an interface method is removed or renamed,
// these lines will produce a compile error.
var (
	_ billing.SubscriptionManager = (*billing.LemonSqueezyClient)(nil)
	_ billing.KeyManager          = (*billing.UnkeyClient)(nil)
	_ billing.Emailer             = (*billing.EmailClient)(nil)
)

func TestNewService_Success(t *testing.T) {
	cfg := billing.Config{
		LSAPIKey:        "dummy-ls-key",
		LSStoreID:       "12345",
		UnkeyRootKey:    "dummy-unkey-root",
		UnkeyAPIID:      "dummy-unkey-api",
		ResendAPIKey:    "dummy-resend-key",
		ResendFromEmail: "test@example.com",
	}

	svc, err := billing.NewService(cfg)
	if err != nil {
		t.Fatalf("NewService returned unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("NewService returned nil service")
	}
	if svc.LS == nil {
		t.Error("Service.LS is nil")
	}
	if svc.Keys == nil {
		t.Error("Service.Keys is nil")
	}
	if svc.Email == nil {
		t.Error("Service.Email is nil")
	}
}

func TestNewService_ReturnsNonNilFields(t *testing.T) {
	cfg := billing.Config{
		LSAPIKey:        "x",
		LSStoreID:       "1",
		UnkeyRootKey:    "x",
		UnkeyAPIID:      "x",
		ResendAPIKey:    "x",
		ResendFromEmail: "from@example.com",
	}

	svc, err := billing.NewService(cfg)
	if err != nil {
		t.Fatalf("NewService returned unexpected error: %v", err)
	}

	if svc.LS == nil {
		t.Error("Service.LS is nil — SubscriptionManager not wired")
	}
	if svc.Keys == nil {
		t.Error("Service.Keys is nil — KeyManager not wired")
	}
	if svc.Email == nil {
		t.Error("Service.Email is nil — Emailer not wired")
	}
}
