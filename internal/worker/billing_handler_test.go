package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
	sdkerrors "github.com/unkeyed/unkey-go/models/sdkerrors"
)

// --- mock BillingRevokeKeyStore ---

type mockBillingRevokeKeyStore struct {
	getKeyFunc    func(ctx context.Context, lsSubID string) (string, error)
	expireSubFunc func(ctx context.Context, lsSubID string) error
}

func (m *mockBillingRevokeKeyStore) GetSubscriptionKeyID(ctx context.Context, lsSubID string) (string, error) {
	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, lsSubID)
	}
	return "uk_default", nil
}

func (m *mockBillingRevokeKeyStore) ExpireSubscription(ctx context.Context, lsSubID string) error {
	if m.expireSubFunc != nil {
		return m.expireSubFunc(ctx, lsSubID)
	}
	return nil
}

// --- mock KeyManager ---

type mockKeyManager struct {
	revokeKeyFunc func(ctx context.Context, keyID string) error
}

func (m *mockKeyManager) CreateKey(_ context.Context, _, _ string) (string, string, error) {
	return "", "", nil
}

func (m *mockKeyManager) UpdateKeyTier(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockKeyManager) RevokeKey(ctx context.Context, keyID string) error {
	if m.revokeKeyFunc != nil {
		return m.revokeKeyFunc(ctx, keyID)
	}
	return nil
}

// --- helpers ---

func newRevokeKeyTask(t *testing.T, p billingRevokeKeyPayload) *asynq.Task {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	// Use the canonical constant — never a string literal — to catch type drift.
	return asynq.NewTask(TypeBillingRevokeKey, data)
}

func newHandler(store BillingRevokeKeyStore, keys *mockKeyManager) *BillingRevokeKeyHandler {
	return &BillingRevokeKeyHandler{
		store: store,
		keys:  keys,
		log:   zerolog.Nop(),
	}
}

// --- tests ---

// TestBillingRevokeKeyHandler_HappyPath_KeyInPayload verifies that when
// UnkeyKeyID is present in the payload, RevokeKey is called directly without
// a DB lookup, and ExpireSubscription is called afterwards.
func TestBillingRevokeKeyHandler_HappyPath_KeyInPayload(t *testing.T) {
	var revokedKeyID string
	var expiredSubID string

	store := &mockBillingRevokeKeyStore{
		getKeyFunc: func(_ context.Context, _ string) (string, error) {
			t.Error("GetSubscriptionKeyID should not be called when UnkeyKeyID is in payload")
			return "", nil
		},
		expireSubFunc: func(_ context.Context, lsSubID string) error {
			expiredSubID = lsSubID
			return nil
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, keyID string) error {
			revokedKeyID = keyID
			return nil
		},
	}

	h := newHandler(store, keys)
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{
		UnkeyKeyID:       "uk_abc123",
		LSSubscriptionID: "sub_xyz",
	})

	if err := h.Handle(context.Background(), task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if revokedKeyID != "uk_abc123" {
		t.Errorf("RevokeKey called with %q, want %q", revokedKeyID, "uk_abc123")
	}
	if expiredSubID != "sub_xyz" {
		t.Errorf("ExpireSubscription called with %q, want %q", expiredSubID, "sub_xyz")
	}
}

// TestBillingRevokeKeyHandler_HappyPath_KeyFromDB verifies that when
// UnkeyKeyID is absent from the payload, the handler fetches it via
// GetSubscriptionKeyID and then revokes it.
func TestBillingRevokeKeyHandler_HappyPath_KeyFromDB(t *testing.T) {
	var revokedKeyID string
	var lookedUpSubID string

	store := &mockBillingRevokeKeyStore{
		getKeyFunc: func(_ context.Context, lsSubID string) (string, error) {
			lookedUpSubID = lsSubID
			return "uk_from_db", nil
		},
		expireSubFunc: func(_ context.Context, _ string) error { return nil },
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, keyID string) error {
			revokedKeyID = keyID
			return nil
		},
	}

	h := newHandler(store, keys)
	// UnkeyKeyID intentionally omitted — handler must fall back to DB.
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{
		LSSubscriptionID: "sub_fromdb",
	})

	if err := h.Handle(context.Background(), task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lookedUpSubID != "sub_fromdb" {
		t.Errorf("GetSubscriptionKeyID called with %q, want %q", lookedUpSubID, "sub_fromdb")
	}
	if revokedKeyID != "uk_from_db" {
		t.Errorf("RevokeKey called with %q, want %q", revokedKeyID, "uk_from_db")
	}
}

// TestBillingRevokeKeyHandler_MissingSubscriptionID verifies that a payload
// lacking LSSubscriptionID is rejected before any external call is made.
func TestBillingRevokeKeyHandler_MissingSubscriptionID(t *testing.T) {
	store := &mockBillingRevokeKeyStore{
		getKeyFunc: func(_ context.Context, _ string) (string, error) {
			t.Error("GetSubscriptionKeyID should not be called")
			return "", nil
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, _ string) error {
			t.Error("RevokeKey should not be called")
			return nil
		},
	}

	h := newHandler(store, keys)
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{UnkeyKeyID: "uk_abc"}) // no LSSubscriptionID

	if err := h.Handle(context.Background(), task); err == nil {
		t.Error("expected error for missing LSSubscriptionID, got nil")
	}
}

// TestBillingRevokeKeyHandler_InvalidJSON verifies that a malformed payload
// returns an unmarshal error without calling any downstream dependency.
func TestBillingRevokeKeyHandler_InvalidJSON(t *testing.T) {
	h := newHandler(&mockBillingRevokeKeyStore{}, &mockKeyManager{})
	task := asynq.NewTask(TypeBillingRevokeKey, []byte("not-json"))

	if err := h.Handle(context.Background(), task); err == nil {
		t.Error("expected unmarshal error, got nil")
	}
}

// TestBillingRevokeKeyHandler_RevokeKeyError verifies that a non-ErrNotFound
// RevokeKey failure propagates as an error from Handle without calling ExpireSubscription.
func TestBillingRevokeKeyHandler_RevokeKeyError(t *testing.T) {
	store := &mockBillingRevokeKeyStore{
		expireSubFunc: func(_ context.Context, _ string) error {
			t.Error("ExpireSubscription should not be called after non-ErrNotFound RevokeKey failure")
			return nil
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, _ string) error {
			return errors.New("unkey API error")
		},
	}

	h := newHandler(store, keys)
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{
		UnkeyKeyID:       "uk_fail",
		LSSubscriptionID: "sub_fail",
	})

	if err := h.Handle(context.Background(), task); err == nil {
		t.Error("expected error from RevokeKey failure, got nil")
	}
}

// TestBillingRevokeKeyHandler_AlreadyRevoked_Direct verifies that when RevokeKey
// returns *sdkerrors.ErrNotFound directly (the concrete type the Unkey SDK returns for
// a 404 JSON response), the handler treats it as a no-op and still calls ExpireSubscription.
func TestBillingRevokeKeyHandler_AlreadyRevoked_Direct(t *testing.T) {
	var expiredSubID string

	store := &mockBillingRevokeKeyStore{
		expireSubFunc: func(_ context.Context, lsSubID string) error {
			expiredSubID = lsSubID
			return nil
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, _ string) error {
			return &sdkerrors.ErrNotFound{}
		},
	}

	h := newHandler(store, keys)
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{
		UnkeyKeyID:       "uk_already_gone",
		LSSubscriptionID: "sub_retry",
	})

	if err := h.Handle(context.Background(), task); err != nil {
		t.Fatalf("expected no error for already-revoked key (direct), got: %v", err)
	}
	if expiredSubID != "sub_retry" {
		t.Errorf("ExpireSubscription called with %q, want %q", expiredSubID, "sub_retry")
	}
}

// TestBillingRevokeKeyHandler_AlreadyRevoked_Wrapped verifies that errors.As
// correctly unwraps a fmt.Errorf("%w")-wrapped *sdkerrors.ErrNotFound, matching
// the actual wrapping that billing.UnkeyClient.RevokeKey applies.
// This guards the production code path where the SDK error is wrapped before
// the handler receives it.
func TestBillingRevokeKeyHandler_AlreadyRevoked_Wrapped(t *testing.T) {
	var expiredSubID string

	store := &mockBillingRevokeKeyStore{
		expireSubFunc: func(_ context.Context, lsSubID string) error {
			expiredSubID = lsSubID
			return nil
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, _ string) error {
			// Mirrors the wrapping billing.UnkeyClient.RevokeKey applies:
			// fmt.Errorf("billing: revoke key: %w", sdkErr)
			return fmt.Errorf("billing: revoke key: %w", &sdkerrors.ErrNotFound{})
		},
	}

	h := newHandler(store, keys)
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{
		UnkeyKeyID:       "uk_wrapped_gone",
		LSSubscriptionID: "sub_retry_wrapped",
	})

	if err := h.Handle(context.Background(), task); err != nil {
		t.Fatalf("expected no error for wrapped already-revoked key, got: %v", err)
	}
	if expiredSubID != "sub_retry_wrapped" {
		t.Errorf("ExpireSubscription called with %q, want %q", expiredSubID, "sub_retry_wrapped")
	}
}

// TestBillingRevokeKeyHandler_ExpireSubscriptionError verifies that a DB
// failure after a successful key revocation propagates as an error from Handle.
func TestBillingRevokeKeyHandler_ExpireSubscriptionError(t *testing.T) {
	store := &mockBillingRevokeKeyStore{
		expireSubFunc: func(_ context.Context, _ string) error {
			return errors.New("db write error")
		},
	}
	keys := &mockKeyManager{
		revokeKeyFunc: func(_ context.Context, _ string) error { return nil },
	}

	h := newHandler(store, keys)
	task := newRevokeKeyTask(t, billingRevokeKeyPayload{
		UnkeyKeyID:       "uk_ok",
		LSSubscriptionID: "sub_db_err",
	})

	if err := h.Handle(context.Background(), task); err == nil {
		t.Error("expected error from ExpireSubscription failure, got nil")
	}
}
