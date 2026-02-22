package billing_test

import (
	"os"
	"testing"

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

	keyID, keyValue, err := client.CreateKey("integration-test@example.com", "free")
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
		if rErr := client.RevokeKey(keyID); rErr != nil {
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

	// Create a key to update.
	keyID, _, err := client.CreateKey("tier-update-test@example.com", "free")
	if err != nil {
		t.Fatalf("CreateKey returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_ = client.RevokeKey(keyID)
	})

	if err := client.UpdateKeyTier(keyID, "developer"); err != nil {
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

	// Create a key specifically to revoke.
	keyID, _, err := client.CreateKey("revoke-test@example.com", "free")
	if err != nil {
		t.Fatalf("CreateKey returned unexpected error: %v", err)
	}

	if err := client.RevokeKey(keyID); err != nil {
		t.Fatalf("RevokeKey returned unexpected error: %v", err)
	}
}
