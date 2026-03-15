package signup_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"llm-pricing-api/internal/signup"
)

// ─── In-memory mock store ─────────────────────────────────────────────────────

type mockStore struct {
	identities map[string]*signup.Identity // key = lower(email)
	tokens     map[string]*signup.MagicLinkToken
	keys       map[string]*signup.KeyRecord // key = identityID
}

func newMock() *mockStore {
	return &mockStore{
		identities: make(map[string]*signup.Identity),
		tokens:     make(map[string]*signup.MagicLinkToken),
		keys:       make(map[string]*signup.KeyRecord),
	}
}

func (m *mockStore) UpsertIdentity(_ context.Context, email, ipHash, uaHash string) (*signup.Identity, error) {
	key := strings.ToLower(email) // normalize to lower-case, matching production store
	if id, ok := m.identities[key]; ok {
		return id, nil
	}
	id := &signup.Identity{
		ID:        "id-" + key,
		Email:     email,
		IPHash:    ipHash,
		UAHash:    uaHash,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.identities[key] = id
	return id, nil
}

func (m *mockStore) GetIdentityByEmail(_ context.Context, email string) (*signup.Identity, error) {
	id, ok := m.identities[strings.ToLower(email)] // normalize, matching production
	if !ok {
		return nil, signup.ErrNotFound
	}
	return id, nil
}

func (m *mockStore) GetIdentityByID(_ context.Context, id string) (*signup.Identity, error) {
	for _, ident := range m.identities {
		if ident.ID == id {
			return ident, nil
		}
	}
	return nil, signup.ErrNotFound
}

func (m *mockStore) MarkEmailVerified(_ context.Context, identityID string) error {
	now := time.Now()
	for _, id := range m.identities {
		if id.ID == identityID {
			id.EmailVerifiedAt = &now
			return nil
		}
	}
	return signup.ErrNotFound
}

func (m *mockStore) InsertToken(_ context.Context, identityID, tokenHash string, expiresAt time.Time) (*signup.MagicLinkToken, error) {
	t := &signup.MagicLinkToken{
		ID:         "tok-" + tokenHash,
		IdentityID: identityID,
		TokenHash:  tokenHash,
		ExpiresAt:  expiresAt,
		CreatedAt:  time.Now(),
	}
	m.tokens[tokenHash] = t
	return t, nil
}

func (m *mockStore) ConsumeToken(_ context.Context, tokenHash string) (*signup.MagicLinkToken, error) {
	t, ok := m.tokens[tokenHash]
	if !ok {
		return nil, signup.ErrNotFound
	}
	if t.UsedAt != nil {
		return nil, signup.ErrTokenConsumed
	}
	if time.Now().After(t.ExpiresAt) {
		return nil, signup.ErrTokenExpired
	}
	now := time.Now()
	t.UsedAt = &now
	return t, nil
}

func (m *mockStore) DeleteExpiredTokens(_ context.Context) (int64, error) {
	var deleted int64
	for k, t := range m.tokens {
		if time.Now().After(t.ExpiresAt) && t.UsedAt == nil {
			delete(m.tokens, k)
			deleted++
		}
	}
	return deleted, nil
}

func (m *mockStore) GetActiveKey(_ context.Context, identityID string) (*signup.KeyRecord, error) {
	k, ok := m.keys[identityID]
	if !ok || k.Status != "active" {
		return nil, signup.ErrNotFound
	}
	return k, nil
}

func (m *mockStore) InsertKey(_ context.Context, identityID, providerKeyID string) (*signup.KeyRecord, error) {
	if existing, ok := m.keys[identityID]; ok && existing.Status == "active" {
		return nil, signup.ErrDuplicateActiveKey
	}
	k := &signup.KeyRecord{
		ID:            "key-" + identityID,
		IdentityID:    identityID,
		ProviderKeyID: providerKeyID,
		Status:        "active",
		CreatedAt:     time.Now(),
	}
	m.keys[identityID] = k
	return k, nil
}

func (m *mockStore) RevokeKey(_ context.Context, identityID, _ string) error {
	k, ok := m.keys[identityID]
	if !ok || k.Status != "active" {
		return signup.ErrNotFound
	}
	now := time.Now()
	k.Status = "revoked"
	k.RevokedAt = &now
	return nil
}

func (m *mockStore) RevokeAndInsertKey(_ context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*signup.KeyRecord, error) {
	// Revoke old (best-effort, mirrors real implementation).
	if oldProviderKeyID != "" {
		if k, ok := m.keys[identityID]; ok && k.Status == "active" {
			now := time.Now()
			k.Status = "revoked"
			k.RevokedAt = &now
		}
	}
	// Insert new.
	return m.InsertKey(context.Background(), identityID, newProviderKeyID)
}

// ─── Mock store tests ─────────────────────────────────────────────────────────

func TestUpsertIdentity_NewAndIdempotent(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id1, err := store.UpsertIdentity(ctx, "alice@example.com", "iphash", "uahash")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id1.Email != "alice@example.com" {
		t.Errorf("email mismatch: %q", id1.Email)
	}

	// Second call returns same identity
	id2, err := store.UpsertIdentity(ctx, "alice@example.com", "", "")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id1.ID != id2.ID {
		t.Errorf("expected same id, got %q vs %q", id1.ID, id2.ID)
	}
}

func TestGetIdentityByEmail_NotFound(t *testing.T) {
	store := newMock()
	_, err := store.GetIdentityByEmail(context.Background(), "nobody@example.com")
	if !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMarkEmailVerified(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "bob@example.com", "", "")
	if id.EmailVerifiedAt != nil {
		t.Fatal("expected unverified initially")
	}

	if err := store.MarkEmailVerified(ctx, id.ID); err != nil {
		t.Fatalf("mark verified: %v", err)
	}

	id2, _ := store.GetIdentityByID(ctx, id.ID)
	if id2.EmailVerifiedAt == nil {
		t.Error("expected email_verified_at to be set")
	}
}

func TestToken_HappyPath(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "carol@example.com", "", "")
	expiresAt := time.Now().Add(15 * time.Minute)

	tok, err := store.InsertToken(ctx, id.ID, "hash123", expiresAt)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	if tok.UsedAt != nil {
		t.Error("new token should not be used")
	}

	// Consume once — should succeed
	consumed, err := store.ConsumeToken(ctx, "hash123")
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if consumed.UsedAt == nil {
		t.Error("consumed token should have used_at set")
	}
}

func TestToken_OneTimeUse(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "dave@example.com", "", "")
	expiresAt := time.Now().Add(15 * time.Minute)
	_, _ = store.InsertToken(ctx, id.ID, "hash456", expiresAt)
	_, _ = store.ConsumeToken(ctx, "hash456") // first use

	// Second consumption must fail
	_, err := store.ConsumeToken(ctx, "hash456")
	if !errors.Is(err, signup.ErrTokenConsumed) {
		t.Errorf("expected ErrTokenConsumed, got %v", err)
	}
}

func TestToken_Expired(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "eve@example.com", "", "")
	// Token already expired
	expiresAt := time.Now().Add(-1 * time.Second)
	_, _ = store.InsertToken(ctx, id.ID, "expiredhash", expiresAt)

	_, err := store.ConsumeToken(ctx, "expiredhash")
	if !errors.Is(err, signup.ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestToken_NotFound(t *testing.T) {
	store := newMock()
	_, err := store.ConsumeToken(context.Background(), "nonexistent")
	if !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestKey_InsertAndGet(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "frank@example.com", "", "")
	k, err := store.InsertKey(ctx, id.ID, "unkey_key_id_1")
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}
	if k.Status != "active" {
		t.Errorf("expected active, got %q", k.Status)
	}

	got, err := store.GetActiveKey(ctx, id.ID)
	if err != nil {
		t.Fatalf("get active key: %v", err)
	}
	if got.ProviderKeyID != "unkey_key_id_1" {
		t.Errorf("provider key mismatch: %q", got.ProviderKeyID)
	}
}

func TestKey_OneActivePerIdentity(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "grace@example.com", "", "")
	_, _ = store.InsertKey(ctx, id.ID, "first_key")

	// Second insert should be blocked
	_, err := store.InsertKey(ctx, id.ID, "second_key")
	if !errors.Is(err, signup.ErrDuplicateActiveKey) {
		t.Errorf("expected ErrDuplicateActiveKey, got %v", err)
	}
}

func TestKey_RevokeAllowsNewKey(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "hank@example.com", "", "")
	_, _ = store.InsertKey(ctx, id.ID, "old_key")
	if err := store.RevokeKey(ctx, id.ID, "old_key"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// After revocation a new key can be inserted
	k2, err := store.InsertKey(ctx, id.ID, "new_key")
	if err != nil {
		t.Fatalf("insert after revoke: %v", err)
	}
	if k2.Status != "active" {
		t.Errorf("expected active, got %q", k2.Status)
	}
}

func TestDeleteExpiredTokens(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "ivan@example.com", "", "")
	_, _ = store.InsertToken(ctx, id.ID, "expired1", time.Now().Add(-time.Second))
	_, _ = store.InsertToken(ctx, id.ID, "valid1", time.Now().Add(time.Hour))

	n, err := store.DeleteExpiredTokens(ctx)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}

	// valid token still consumable
	if _, err := store.ConsumeToken(ctx, "valid1"); err != nil {
		t.Errorf("valid token should still be consumable: %v", err)
	}
}

// ── HashToken ─────────────────────────────────────────────────────────────────

func TestHashToken_Deterministic(t *testing.T) {
	raw := "test-token-abc123"
	h1 := signup.HashToken(raw)
	h2 := signup.HashToken(raw)
	if h1 != h2 {
		t.Errorf("HashToken is not deterministic: %q vs %q", h1, h2)
	}
}

func TestHashToken_DifferentInputsDifferentOutputs(t *testing.T) {
	h1 := signup.HashToken("token-a")
	h2 := signup.HashToken("token-b")
	if h1 == h2 {
		t.Error("HashToken collision: different inputs produced same hash")
	}
}

func TestHashToken_EmptyString(t *testing.T) {
	h := signup.HashToken("")
	if h == "" {
		t.Error("HashToken of empty string should not be empty")
	}
}

func TestHashToken_FixedVector(t *testing.T) {
	cases := []struct {
		raw    string
		prefix string // first 8 hex chars of expected SHA-256
	}{
		{"", "e3b0c442"},
		{"abc", "ba7816bf"},
	}
	for _, tc := range cases {
		got := signup.HashToken(tc.raw)
		if len(got) < 8 || got[:8] != tc.prefix {
			t.Errorf("HashToken(%q): got %q, want prefix %q", tc.raw, got, tc.prefix)
		}
	}
}

// ── Sentinel error tests ──────────────────────────────────────────────────────

func TestSentinels_AreDistinct(t *testing.T) {
	if errors.Is(signup.ErrNotFound, signup.ErrTokenConsumed) {
		t.Error("ErrNotFound and ErrTokenConsumed must be distinct")
	}
	if errors.Is(signup.ErrNotFound, signup.ErrTokenExpired) {
		t.Error("ErrNotFound and ErrTokenExpired must be distinct")
	}
	if errors.Is(signup.ErrTokenConsumed, signup.ErrTokenExpired) {
		t.Error("ErrTokenConsumed and ErrTokenExpired must be distinct")
	}
}

// ── Zero-value sanity ─────────────────────────────────────────────────────────

func TestKeyRecord_ZeroValue(t *testing.T) {
	var k signup.KeyRecord
	if k.Status != "" {
		t.Error("zero-value KeyRecord should have empty Status")
	}
	if k.RevokedAt != nil {
		t.Error("zero-value KeyRecord.RevokedAt should be nil")
	}
}

func TestMagicLinkToken_ZeroValue(t *testing.T) {
	var tok signup.MagicLinkToken
	if tok.UsedAt != nil {
		t.Error("zero-value MagicLinkToken.UsedAt should be nil (unused)")
	}
	if !tok.ExpiresAt.IsZero() {
		t.Error("zero-value MagicLinkToken.ExpiresAt should be zero time")
	}
}
