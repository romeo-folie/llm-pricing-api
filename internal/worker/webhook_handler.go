package worker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"llm-pricing-api/internal/webhooks"
)

// WebhookPayload is an alias for webhooks.Payload for backward compatibility
// with callers that reference the worker package.
type WebhookPayload = webhooks.Payload

// WebhookTaskPayload is an alias for webhooks.TaskPayload for backward
// compatibility with callers that reference the worker package.
type WebhookTaskPayload = webhooks.TaskPayload

// NewWebhookDeliverTask creates an asynq task for webhook delivery.
func NewWebhookDeliverTask(payload WebhookTaskPayload) (*asynq.Task, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook task payload: %w", err)
	}
	return asynq.NewTask(TypeWebhookDeliver, data,
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Second),
	), nil
}

// WebhookDeliveryHandler executes TypeWebhookDeliver asynq tasks.
// It decrypts the per-webhook secret from the task payload before computing
// the HMAC-SHA256 signature, so the delivery pipeline always operates on the
// plaintext secret regardless of how it was stored at rest.
type WebhookDeliveryHandler struct {
	// secretKey is the hex-encoded 32-byte AES-256-GCM key used to decrypt
	// webhook secrets stored encrypted in the database. If empty, secrets are
	// treated as plaintext (ephemeral-key mode where WEBHOOK_SECRET_KEY is unset).
	secretKey string
}

// NewWebhookDeliveryHandler constructs a handler that decrypts secrets with
// the given hex-encoded 32-byte AES key. Pass an empty string to accept
// plaintext secrets (ephemeral-key mode).
func NewWebhookDeliveryHandler(secretKey string) *WebhookDeliveryHandler {
	return &WebhookDeliveryHandler{secretKey: secretKey}
}

// Handle is the asynq handler for TypeWebhookDeliver tasks.
// It decrypts the per-webhook secret, signs the serialised event body with
// HMAC-SHA256, POSTs it to the registered URL, and returns an error on
// non-2xx status so asynq retries the task.
func (h *WebhookDeliveryHandler) Handle(ctx context.Context, t *asynq.Task) error {
	var payload WebhookTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal webhook task: %w", err)
	}

	// Decrypt the secret that was stored encrypted at rest before computing
	// the HMAC. Without this, the signature would use the ciphertext as key,
	// producing values that no receiver can verify.
	plainSecret, err := webhooks.DecryptSecret(payload.Secret, h.secretKey)
	if err != nil {
		return fmt.Errorf("decrypt webhook secret: %w", err)
	}

	eventJSON, err := json.Marshal(payload.Event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// HMAC-SHA256 signature over the serialised event body using the plaintext secret.
	mac := hmac.New(sha256.New, []byte(plainSecret))
	mac.Write(eventJSON)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, payload.URL, strings.NewReader(string(eventJSON)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LLMPricing-Signature", sig)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook delivery failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook destination returned non-2xx: %d", resp.StatusCode)
	}
	return nil
}
