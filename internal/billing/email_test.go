package billing_test

import (
	"testing"
	"time"

	"llm-pricing-api/internal/billing"
)

func TestNewEmailClient_Success(t *testing.T) {
	// NewEmailClient should succeed because the embedded HTML templates are
	// bundled at compile time and will always be present.
	client, err := billing.NewEmailClient("dummy-resend-key", "from@example.com")
	if err != nil {
		t.Fatalf("NewEmailClient returned unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("NewEmailClient returned nil client")
	}
}

func TestSendKeyDelivery_InvalidAPIKey(t *testing.T) {
	client, err := billing.NewEmailClient("invalid-key", "from@example.com")
	if err != nil {
		t.Fatalf("NewEmailClient returned unexpected error: %v", err)
	}

	// Calling Send with an invalid API key should cause the Resend client to
	// return an error. No real email is sent.
	err = client.SendKeyDelivery("to@example.com", "free", "test_api_key_value")
	if err == nil {
		t.Fatal("SendKeyDelivery: expected error with invalid API key, got nil")
	}
}

func TestSendPlanChange_InvalidAPIKey(t *testing.T) {
	client, err := billing.NewEmailClient("invalid-key", "from@example.com")
	if err != nil {
		t.Fatalf("NewEmailClient returned unexpected error: %v", err)
	}

	renewsAt := time.Now().Add(30 * 24 * time.Hour)
	err = client.SendPlanChange("to@example.com", "free", "developer", renewsAt)
	if err == nil {
		t.Fatal("SendPlanChange: expected error with invalid API key, got nil")
	}
}

func TestSendCancellation_InvalidAPIKey(t *testing.T) {
	client, err := billing.NewEmailClient("invalid-key", "from@example.com")
	if err != nil {
		t.Fatalf("NewEmailClient returned unexpected error: %v", err)
	}

	accessUntil := time.Now().Add(30 * 24 * time.Hour)
	err = client.SendCancellation("to@example.com", accessUntil)
	if err == nil {
		t.Fatal("SendCancellation: expected error with invalid API key, got nil")
	}
}
