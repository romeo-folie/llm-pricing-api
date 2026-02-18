// Package webhooks defines shared types and constants for webhook delivery.
// Both the reconciler (which enqueues tasks) and the worker (which executes
// them) depend on this package, avoiding an import cycle.
package webhooks

import "time"

// TypeWebhookDeliver is the asynq task type name for webhook delivery jobs.
const TypeWebhookDeliver = "webhook:deliver"

// Payload is the event body delivered to registered webhook URLs.
type Payload struct {
	ModelID        int       `json:"model_id"`
	Provider       string    `json:"provider"`
	OldPriceInput  float64   `json:"old_price_input"`
	OldPriceOutput float64   `json:"old_price_output"`
	NewPriceInput  float64   `json:"new_price_input"`
	NewPriceOutput float64   `json:"new_price_output"`
	ConfirmedAt    time.Time `json:"confirmed_at"`
	Source         string    `json:"source"`
}

// TaskPayload is the asynq task payload (what gets enqueued in Redis).
type TaskPayload struct {
	WebhookID string  `json:"webhook_id"`
	URL       string  `json:"url"`
	Secret    string  `json:"secret"` // encrypted secret; decrypted by the worker
	Event     Payload `json:"event"`
}
