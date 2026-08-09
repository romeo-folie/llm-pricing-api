# internal/webhooks

Shared webhook types, task constants, and secret decryption.

## Purpose

Holds the contract between the two halves of webhook delivery: the **reconciler** enqueues asynq tasks when a price change is confirmed, and the **worker** dequeues and delivers them. Both need the same payload shape and task-type name.

The package exists specifically to break an import cycle — `internal/reconciler` and `internal/worker` would otherwise have to import each other to agree on these types.

## Structure

```
internal/webhooks/
  webhooks.go  # TypeWebhookDeliver, Payload, TaskPayload, DecryptSecret
  README.md    # This file
```

## Key Components

### `TypeWebhookDeliver`

```go
const TypeWebhookDeliver = "webhook:deliver"
```

The asynq task type name. Registered by the worker's `ServeMux` and used by the reconciler when enqueuing.

### `Payload`

The event body delivered to the subscriber's URL:

```go
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
```

These are explicit scalar fields, not an embedded database row. Per the project security rules, delivery jobs must never carry raw DB structs — adding a column must not silently start publishing it to third-party endpoints.

### `TaskPayload`

What is actually enqueued in Redis:

```go
type TaskPayload struct {
    WebhookID string  `json:"webhook_id"`
    URL       string  `json:"url"`
    Secret    string  `json:"secret"` // AES-256-GCM encrypted
    Event     Payload `json:"event"`
}
```

Kept distinct from `Payload` so the transport envelope — subscriber ID, destination, encrypted secret — never leaks into the event body delivered to the endpoint.

### `DecryptSecret`

```go
func DecryptSecret(encSecret, hexKey string) (string, error)
```

Decrypts an AES-256-GCM webhook secret at delivery time. Secrets are stored encrypted at rest; `hexKey` is the hex-encoded 32-byte key from `WEBHOOK_SECRET_KEY`. The worker decrypts only in the delivery handler, so plaintext secrets exist for the shortest possible window and never enter a queue payload or log line.

## Usage

Enqueue from the reconciler after a confirmed change:

```go
task := asynq.NewTask(webhooks.TypeWebhookDeliver, mustJSON(webhooks.TaskPayload{
    WebhookID: hook.ID,
    URL:       hook.URL,
    Secret:    hook.EncryptedSecret,
    Event:     webhooks.Payload{ /* ... */ },
}))
client.Enqueue(task, asynq.MaxRetry(3))
```

Decrypt in the worker's delivery handler:

```go
secret, err := webhooks.DecryptSecret(tp.Secret, h.secretKey)
if err != nil {
    return fmt.Errorf("decrypt webhook secret: %w", err)
}
```

## Design Notes

- **Types only, no behaviour.** Deliberately free of database, HTTP, and asynq imports so both the API and worker can depend on it without pulling in the other's dependencies.
- **`WEBHOOK_SECRET_KEY` is optional but ephemeral if unset.** `cmd/api` generates a random key and logs a warning; secrets encrypted under it will not survive a restart. Set it explicitly in any environment where webhooks matter.
- **Delivery guarantees** (at-least-once, 3 attempts, exponential backoff) are configured by the enqueuing and handling code in `internal/reconciler` and `internal/worker`, not here.

## Dependencies

Standard library only (`crypto/aes`, `crypto/cipher`, `encoding/hex`, `fmt`, `time`).

Consumed by [`internal/reconciler`](../reconciler/README.md) (enqueue side) and [`internal/worker`](../worker/README.md) (delivery side). Registration handlers live in `internal/api/handlers/webhooks.go`.
