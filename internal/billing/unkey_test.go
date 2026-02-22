package billing_test

import (
	"context"
	"os"
	"testing"
	"time"

	"llm-pricing-api/internal/billing"
)

func TestNewUnkeyClient_NotNil(t *testing.T) {
	client := billing.NewUnkeyClient("dummy-root-key", "dummy-api-id")
	if client == nil {
		t.Fatal("NewUnkeyClient returned nil")
	}
}

func TestUnkeyClient_CreateKey_Integration(t *testing.T) {
	rootKey := os.Getenv("UNKEY_ROOT_KEY")
	if rootKey == "" {
		t.Skip("UNKEY_ROOT_KEY not set — skipping integration test")
	}
	apiID := os.Getenv("UNKEY_API_ID")
	if apiID == "" {
		t.Skip("UNKEY_API_ID not set — skipping integration test")
	}

	client := billing.NewUnkeyClient(rootKey, apiID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	keyID, keyValue, err := client.CreateKey(ctx, "integration-test@example.com", "free")
	if err != nil {
		t.Fatalf("CreateKey returned unexpected error: %v", err)
	}
	if keyID == "" {
		t.Error("CreateKey returned empty keyID")
	}
	if keyValue == "" {
		t.Error("CreateKey returned empty keyValue")
	}

	// Clean up: revoke the test key we just created.
	t.Cleanup(func() {
		if rErr := client.RevokeKey(ctx, keyID); rErr != nil {
			t.Logf("cleanup: RevokeKey(%q) error (non-fatal): %v", keyID, rErr)
		}
	})
}

func TestUnkeyClient_UpdateKeyTier_Integration(t *testing.T) {
	rootKey := os.Getenv("UNKEY_ROOT_KEY")
	if rootKey == "" {
		t.Skip("UNKEY_ROOT_KEY not set — skipping integration test")
	}
	apiID := os.Getenv("UNKEY_API_ID")
	if apiID == "" {
		t.Skip("UNKEY_API_ID not set — skipping integration test")
	}

	client := billing.NewUnkeyClient(rootKey, apiID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	// Create a key to update.
	keyID, _, err := client.CreateKey(ctx, "tier-update-test@example.com", "free")
	if err != nil {
		t.Fatalf("CreateKey returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_ = client.RevokeKey(ctx, keyID)
	})

	if err := client.UpdateKeyTier(ctx, keyID, "developer"); err != nil {
		t.Fatalf("UpdateKeyTier returned unexpected error: %v", err)
	}
}

func TestUnkeyClient_RevokeKey_Integration(t *testing.T) {
	rootKey := os.Getenv("UNKEY_ROOT_KEY")
	if rootKey == "" {
		t.Skip("UNKEY_ROOT_KEY not set — skipping integration test")
	}
	apiID := os.Getenv("UNKEY_API_ID")
	if apiID == "" {
		t.Skip("UNKEY_API_ID not set — skipping integration test")
	}

	client := billing.NewUnkeyClient(rootKey, apiID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	// Create a key specifically to revoke.
	keyID, _, err := client.CreateKey(ctx, "revoke-test@example.com", "free")
	if err != nil {
		t.Fatalf("CreateKey returned unexpected error: %v", err)
	}

	if err := client.RevokeKey(ctx, keyID); err != nil {
		t.Fatalf("RevokeKey returned unexpected error: %v", err)
	}
}
