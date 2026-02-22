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

func validConfig() billing.Config {
	return billing.Config{
		LSAPIKey:        "dummy-ls-key",
		LSStoreID:       "12345",
		UnkeyRootKey:    "dummy-unkey-root",
		UnkeyAPIID:      "dummy-unkey-api",
		ResendAPIKey:    "dummy-resend-key",
		ResendFromEmail: "test@example.com",
		DocsURL:         "https://llmrates.com/docs",
	}
}

func TestNewService_Success(t *testing.T) {
	svc, err := billing.NewService(validConfig())
	if err != nil {
		t.Fatalf("NewService returned unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("NewService returned nil service")
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

func TestNewService_MissingUnkeyRootKey(t *testing.T) {
	cfg := validConfig()
	cfg.UnkeyRootKey = ""
	_, err := billing.NewService(cfg)
	if err == nil {
		t.Fatal("NewService: expected error for missing UnkeyRootKey, got nil")
	}
}

func TestNewService_MissingUnkeyAPIID(t *testing.T) {
	cfg := validConfig()
	cfg.UnkeyAPIID = ""
	_, err := billing.NewService(cfg)
	if err == nil {
		t.Fatal("NewService: expected error for missing UnkeyAPIID, got nil")
	}
}

func TestNewService_MissingLSAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LSAPIKey = ""
	_, err := billing.NewService(cfg)
	if err == nil {
		t.Fatal("NewService: expected error for missing LSAPIKey, got nil")
	}
}

func TestNewService_DefaultDocsURL(t *testing.T) {
	cfg := validConfig()
	cfg.DocsURL = "" // should default to llmrates.com/docs — no error
	_, err := billing.NewService(cfg)
	if err != nil {
		t.Fatalf("NewService: unexpected error with empty DocsURL: %v", err)
	}
}
