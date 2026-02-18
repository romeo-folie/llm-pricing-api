// Package webhooks defines shared types, constants, and crypto helpers for
// webhook delivery. Both the reconciler (which enqueues tasks) and the worker
// (which executes them) depend on this package, avoiding an import cycle.
package webhooks

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"time"
)

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
	Secret    string  `json:"secret"` // AES-256-GCM encrypted; decrypted by the worker
	Event     Payload `json:"event"`
}

// DecryptSecret decrypts a hex-encoded AES-256-GCM ciphertext (nonce || ciphertext)
// using the provided hex-encoded 32-byte key.
//
// If hexKey is empty, the value is returned as-is — this covers deployments
// that have not configured WEBHOOK_SECRET_KEY (ephemeral-key mode).
//
// If the value cannot be hex-decoded or decrypted, it is returned verbatim as
// a plaintext fallback, which handles rows written before encryption was enabled.
func DecryptSecret(encSecret, hexKey string) (string, error) {
	if hexKey == "" {
		return encSecret, nil
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid WEBHOOK_SECRET_KEY: must be 64-char hex-encoded 32-byte key")
	}

	data, err := hex.DecodeString(encSecret)
	if err != nil {
		// Not hex-encoded — treat as plaintext (legacy row).
		return encSecret, nil
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		// Too short to be a valid GCM ciphertext — treat as plaintext.
		return encSecret, nil
	}

	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Decryption failure — fall back to plaintext (handles pre-encryption rows).
		return encSecret, nil
	}
	return string(plaintext), nil
}
