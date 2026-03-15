package signup

import (
	"context"
	"fmt"

	unkeygo "github.com/unkeyed/unkey-go"
	"github.com/unkeyed/unkey-go/models/operations"
)

// KeyIssuer abstracts Unkey key creation and revocation for testability.
type KeyIssuer interface {
	// CreateKey creates a new free-tier key in Unkey and returns the key.id
	// (provider_key_id) and the plaintext key (shown once to the user).
	CreateKey(ctx context.Context, apiID, ownedByID string) (providerKeyID, plaintext string, err error)
	// RevokeKey permanently revokes an Unkey key by its provider key ID.
	RevokeKey(ctx context.Context, providerKeyID string) error
}

// unkeyIssuer implements KeyIssuer against the real Unkey API.
type unkeyIssuer struct {
	client *unkeygo.Unkey
	apiID  string
}

// NewUnkeyIssuer creates an Unkey-backed KeyIssuer.
// rootKey is the UNKEY_ROOT_KEY; apiID is the UNKEY_API_ID.
func NewUnkeyIssuer(rootKey, apiID string) KeyIssuer {
	return &unkeyIssuer{
		client: unkeygo.New(unkeygo.WithSecurity(rootKey)),
		apiID:  apiID,
	}
}

// CreateKey creates a free-tier Unkey key for the given identity.
// ownedByID is the identity UUID (for audit / key lookup).
func (u *unkeyIssuer) CreateKey(ctx context.Context, apiID, ownedByID string) (string, string, error) {
	if apiID == "" {
		apiID = u.apiID
	}
	prefix := ptr("llmr")
	tier := ptr("free")
	resp, err := u.client.Keys.CreateKey(ctx, operations.CreateKeyRequestBody{
		APIID:   apiID,
		OwnerID: ptr(ownedByID),
		Prefix:  prefix,
		Meta: map[string]any{
			"tier": tier,
		},
		Remaining: nil, // no call cap for free tier
	})
	if err != nil {
		return "", "", fmt.Errorf("unkey create key: %w", err)
	}
	if resp.Object == nil {
		return "", "", fmt.Errorf("unkey create key: nil response object")
	}
	return resp.Object.KeyID, resp.Object.Key, nil
}

// RevokeKey permanently deletes the Unkey key identified by providerKeyID.
func (u *unkeyIssuer) RevokeKey(ctx context.Context, providerKeyID string) error {
	_, err := u.client.Keys.DeleteKey(ctx, operations.DeleteKeyRequestBody{
		KeyID: providerKeyID,
	})
	if err != nil {
		return fmt.Errorf("unkey revoke key: %w", err)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
