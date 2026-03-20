package signup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// KeyIssuer abstracts Unkey key creation and revocation for testability.
type KeyIssuer interface {
	// CreateKey creates a new free-tier key in Unkey and returns the key.id
	// (provider_key_id) and the plaintext key (shown once to the user).
	CreateKey(ctx context.Context, apiID, ownedByID string) (providerKeyID, plaintext string, err error)
	// RevokeKey permanently revokes an Unkey key by its provider key ID.
	RevokeKey(ctx context.Context, providerKeyID string) error
}

// unkeyIssuer implements KeyIssuer against Unkey v2 HTTP API.
//
// We intentionally avoid the legacy v1 Go SDK host (api.unkey.dev), which can
// fail DNS resolution in some environments. Auth middleware already uses v2
// directly; signup key issuance should do the same for consistency.
type unkeyIssuer struct {
	rootKey string
	apiID   string
	http    *http.Client
}

// NewUnkeyIssuer creates an Unkey-backed KeyIssuer.
// rootKey is the UNKEY_ROOT_KEY; apiID is the UNKEY_API_ID.
func NewUnkeyIssuer(rootKey, apiID string) KeyIssuer {
	return &unkeyIssuer{
		rootKey: rootKey,
		apiID:   apiID,
		http:    &http.Client{Timeout: 8 * time.Second},
	}
}

// CreateKey creates a free-tier Unkey key for the given identity.
// ownedByID is the identity UUID (for audit / key lookup).
func (u *unkeyIssuer) CreateKey(ctx context.Context, apiID, ownedByID string) (string, string, error) {
	if apiID == "" {
		apiID = u.apiID
	}

	payload := map[string]any{
		"apiId":      apiID,
		"externalId": ownedByID,
		"prefix":     "llmr",
		"meta": map[string]any{
			"tier": "free",
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.unkey.com/v2/keys.createKey", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("unkey create key: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.rootKey)

	resp, err := u.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("unkey create key: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var out struct {
		Data struct {
			KeyID string `json:"keyId"`
			Key   string `json:"key"`
			// Some SDKs/docs shape nested key metadata under "key" object.
			KeyObj struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			} `json:"keyObject"`
		} `json:"data"`
		Error *struct {
			Detail string `json:"detail"`
			Status int    `json:"status"`
			Title  string `json:"title"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", fmt.Errorf("unkey create key: decode: %w", err)
	}
	if out.Error != nil {
		return "", "", fmt.Errorf("unkey create key: %s", out.Error.Detail)
	}
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("unkey create key: status %d", resp.StatusCode)
	}

	keyID := out.Data.KeyID
	key := out.Data.Key
	if keyID == "" {
		keyID = out.Data.KeyObj.ID
	}
	if key == "" {
		key = out.Data.KeyObj.Key
	}
	if keyID == "" || key == "" {
		return "", "", fmt.Errorf("unkey create key: missing keyId/key in response")
	}
	return keyID, key, nil
}

// RevokeKey permanently deletes the Unkey key identified by providerKeyID.
func (u *unkeyIssuer) RevokeKey(ctx context.Context, providerKeyID string) error {
	payload, _ := json.Marshal(map[string]string{"keyId": providerKeyID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.unkey.com/v2/keys.deleteKey", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("unkey revoke key: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.rootKey)

	resp, err := u.http.Do(req)
	if err != nil {
		return fmt.Errorf("unkey revoke key: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var out struct {
		Error *struct {
			Detail string `json:"detail"`
			Status int    `json:"status"`
			Title  string `json:"title"`
		} `json:"error"`
	}
	_ = json.Unmarshal(respBody, &out)
	if out.Error != nil {
		return fmt.Errorf("unkey revoke key: %s", out.Error.Detail)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unkey revoke key: status %d", resp.StatusCode)
	}
	return nil
}
