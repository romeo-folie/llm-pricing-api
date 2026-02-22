package billing

import (
	"context"
	"fmt"

	unkeygo "github.com/unkeyed/unkey-go"
	"github.com/unkeyed/unkey-go/models/operations"
)

// UnkeyClient wraps the Unkey management API for key lifecycle operations.
// It must be constructed via NewUnkeyClient; the zero value is not usable.
type UnkeyClient struct {
	sdk   *unkeygo.Unkey
	apiID string
}

// NewUnkeyClient constructs an UnkeyClient backed by the official Unkey Go SDK.
// rootKey is the Unkey root key used for management operations; apiID is the
// Unkey API identifier under which keys are created.
func NewUnkeyClient(rootKey, apiID string) *UnkeyClient {
	sdk := unkeygo.New(unkeygo.WithSecurity(rootKey))
	return &UnkeyClient{
		sdk:   sdk,
		apiID: apiID,
	}
}

// CreateKey creates a new Unkey API key for the given email address and tier.
// It returns the keyID (a stable reference for future updates/revocations) and
// the plaintext keyValue, which is shown only once. The caller must deliver
// keyValue to the user and must not log or store it.
//
// The key is created with Meta["tier"] = tier, matching the expectation of the
// auth middleware in internal/middleware/auth.go.
func (c *UnkeyClient) CreateKey(email, tier string) (keyID string, keyValue string, err error) {
	name := unkeygo.String(email)
	req := operations.CreateKeyRequestBody{
		APIID: c.apiID,
		Name:  name,
		Meta: map[string]any{
			"tier": tier,
		},
	}

	res, err := c.sdk.Keys.CreateKey(context.Background(), req)
	if err != nil {
		return "", "", fmt.Errorf("billing: create key: %w", err)
	}

	if res.Object == nil {
		return "", "", fmt.Errorf("billing: create key: empty response from Unkey")
	}

	return res.Object.KeyID, res.Object.Key, nil
}

// UpdateKeyTier updates the tier stored in Meta["tier"] on an existing Unkey key.
// unkeyKeyID is the key's stable ID (returned by CreateKey, not the secret value).
// The entire meta map is replaced with {"tier": tier}, so any prior meta is
// intentionally discarded — tier is the only metadata this platform requires.
func (c *UnkeyClient) UpdateKeyTier(unkeyKeyID, tier string) error {
	req := operations.UpdateKeyRequestBody{
		KeyID: unkeyKeyID,
		Meta: map[string]any{
			"tier": tier,
		},
	}

	_, err := c.sdk.Keys.UpdateKey(context.Background(), req)
	if err != nil {
		return fmt.Errorf("billing: update key tier: %w", err)
	}

	return nil
}

// RevokeKey permanently revokes an API key so it can no longer be used for
// authentication. unkeyKeyID is the stable key ID returned by CreateKey.
// Revocation may take up to 30 seconds to propagate across all Unkey regions.
func (c *UnkeyClient) RevokeKey(unkeyKeyID string) error {
	req := operations.DeleteKeyRequestBody{
		KeyID: unkeyKeyID,
	}

	_, err := c.sdk.Keys.DeleteKey(context.Background(), req)
	if err != nil {
		return fmt.Errorf("billing: revoke key: %w", err)
	}

	return nil
}
