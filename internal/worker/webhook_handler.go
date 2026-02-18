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

// HandleWebhookDeliver is the asynq handler for TypeWebhookDeliver tasks.
// It signs the payload with HMAC-SHA256, POSTs it to the registered URL,
// and returns an error on non-2xx status so asynq retries the task.
func HandleWebhookDeliver(ctx context.Context, t *asynq.Task) error {
	var payload WebhookTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal webhook task: %w", err)
	}

	eventJSON, err := json.Marshal(payload.Event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// HMAC-SHA256 signature over the serialised event body.
	mac := hmac.New(sha256.New, []byte(payload.Secret))
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
