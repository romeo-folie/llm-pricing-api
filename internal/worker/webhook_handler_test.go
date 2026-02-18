package worker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWebhookDeliveryHandler_HMACSignatureCorrect verifies that the handler
// sends an X-LLMPricing-Signature header whose value matches a locally-
// computed HMAC-SHA256 of the marshalled event body.
func TestWebhookDeliveryHandler_HMACSignatureCorrect(t *testing.T) {
	secret := "super-secret"
	var gotSig string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-LLMPricing-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	event := WebhookPayload{
		ModelID:        1,
		Provider:       "openai",
		OldPriceInput:  0.000005,
		OldPriceOutput: 0.000015,
		NewPriceInput:  0.000004,
		NewPriceOutput: 0.000012,
		ConfirmedAt:    time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC),
		Source:         "openrouter",
	}

	payload := WebhookTaskPayload{
		WebhookID: "wh-001",
		URL:       srv.URL,
		Secret:    secret, // plaintext — no encryption key configured
		Event:     event,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	task, err := NewWebhookDeliverTask(payload)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	_ = data

	// Empty secretKey → DecryptSecret returns the value as-is (plaintext mode).
	if err := NewWebhookDeliveryHandler("").Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// Compute the expected signature from the received body.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if gotSig != wantSig {
		t.Errorf("signature mismatch\n  got:  %s\n  want: %s", gotSig, wantSig)
	}
}

// TestWebhookDeliveryHandler_Non2xxReturnsError verifies that a non-2xx response
// from the destination URL causes the handler to return an error so asynq
// retries the task.
func TestWebhookDeliveryHandler_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	payload := WebhookTaskPayload{
		WebhookID: "wh-002",
		URL:       srv.URL,
		Secret:    "any-secret",
		Event: WebhookPayload{
			ModelID:     2,
			Provider:    "anthropic",
			ConfirmedAt: time.Now(),
			Source:      "litellm",
		},
	}

	task, err := NewWebhookDeliverTask(payload)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := NewWebhookDeliveryHandler("").Handle(context.Background(), task); err == nil {
		t.Error("expected error for non-2xx response, got nil")
	}
}

// TestWebhookDeliveryHandler_Payload400ReturnsError verifies that a 400 Bad
// Request from the destination is also treated as a failure.
func TestWebhookDeliveryHandler_Payload400ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	payload := WebhookTaskPayload{
		WebhookID: "wh-003",
		URL:       srv.URL,
		Secret:    "another-secret",
		Event: WebhookPayload{
			ModelID:  3,
			Provider: "google",
			Source:   "litellm",
		},
	}

	task, err := NewWebhookDeliverTask(payload)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := NewWebhookDeliveryHandler("").Handle(context.Background(), task); err == nil {
		t.Error("expected error for 400 response, got nil")
	}
}

// TestNewWebhookDeliverTask_TypeIsWebhookDeliver verifies that the created task
// uses the TypeWebhookDeliver constant as its type string.
func TestNewWebhookDeliverTask_TypeIsWebhookDeliver(t *testing.T) {
	payload := WebhookTaskPayload{
		WebhookID: "wh-004",
		URL:       "https://example.com/hook",
		Secret:    "s",
		Event:     WebhookPayload{},
	}

	task, err := NewWebhookDeliverTask(payload)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if task.Type() != TypeWebhookDeliver {
		t.Errorf("expected task type %q, got %q", TypeWebhookDeliver, task.Type())
	}
}
