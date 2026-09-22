package signup_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"llm-pricing-api/internal/signup"
)

// ─── In-memory mock store ─────────────────────────────────────────────────────

type mockStore struct {
	identities map[string]*signup.Identity // key = lower(email)
	tokens     map[string]*signup.MagicLinkToken
	// keys is a slice, not a map keyed by identity: an identity may now hold
	// several active keys (one per agent). Newest is appended last.
	keys []*signup.KeyRecord
}

func newMock() *mockStore {
	return &mockStore{
		identities: make(map[string]*signup.Identity),
		tokens:     make(map[string]*signup.MagicLinkToken),
	}
}

func (m *mockStore) UpsertIdentity(_ context.Context, email, ipHash, uaHash string) (*signup.Identity, error) {
	email = strings.ToLower(strings.TrimSpace(email)) // normalize, matching production store
	if email == "" {
		return nil, errors.New("signup.UpsertIdentity: email must not be empty")
	}
	if id, ok := m.identities[email]; ok {
		return id, nil
	}
	id := &signup.Identity{
		ID:        "id-" + email,
		Email:     email,
		IPHash:    ipHash,
		UAHash:    uaHash,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.identities[email] = id
	return id, nil
}

func (m *mockStore) GetIdentityByEmail(_ context.Context, email string) (*signup.Identity, error) {
	email = strings.ToLower(strings.TrimSpace(email)) // normalize, matching production
	id, ok := m.identities[email]
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
	if !time.Now().Before(t.ExpiresAt) {
		return nil, signup.ErrTokenExpired
	}
	now := time.Now()
	t.UsedAt = &now
	return t, nil
}

func (m *mockStore) DeleteExpiredTokens(_ context.Context) (int64, error) {
	var deleted int64
	for k, t := range m.tokens {
		if !time.Now().Before(t.ExpiresAt) && t.UsedAt == nil {
			delete(m.tokens, k)
			deleted++
		}
	}
	return deleted, nil
}

func (m *mockStore) GetActiveKey(ctx context.Context, identityID string) (*signup.KeyRecord, error) {
	keys, _ := m.ListActiveKeys(ctx, identityID)
	if len(keys) == 0 {
		return nil, signup.ErrNotFound
	}
	return &keys[0], nil
}

// ListActiveKeys mirrors the production ordering: newest first.
func (m *mockStore) ListActiveKeys(_ context.Context, identityID string) ([]signup.KeyRecord, error) {
	out := make([]signup.KeyRecord, 0, 1)
	for i := len(m.keys) - 1; i >= 0; i-- {
		k := m.keys[i]
		if k.IdentityID == identityID && k.Status == "active" {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (m *mockStore) InsertKey(ctx context.Context, identityID, providerKeyID string) (*signup.KeyRecord, error) {
	return m.InsertKeyWithLabel(ctx, identityID, providerKeyID, "", signup.CreatedViaMagicLink)
}

func (m *mockStore) InsertKeyWithLabel(_ context.Context, identityID, providerKeyID, label, createdVia string) (*signup.KeyRecord, error) {
	active, _ := m.ListActiveKeys(context.Background(), identityID)
	if len(active) >= signup.MaxActiveKeysPerIdentity {
		return nil, signup.ErrActiveKeyLimitReached
	}
	if createdVia == "" {
		createdVia = signup.CreatedViaMagicLink
	}
	k := &signup.KeyRecord{
		ID:            "key-" + providerKeyID,
		IdentityID:    identityID,
		ProviderKeyID: providerKeyID,
		Label:         label,
		CreatedVia:    createdVia,
		Status:        "active",
		CreatedAt:     time.Now(),
	}
	m.keys = append(m.keys, k)
	return k, nil
}

func (m *mockStore) RevokeKey(_ context.Context, identityID, providerKeyID string) error {
	for _, k := range m.keys {
		if k.IdentityID == identityID && k.ProviderKeyID == providerKeyID && k.Status == "active" {
			now := time.Now()
			k.Status = "revoked"
			k.RevokedAt = &now
			return nil
		}
	}
	return signup.ErrNotFound
}

func (m *mockStore) RevokeKeyByID(_ context.Context, identityID, keyID string) (string, bool, error) {
	for _, k := range m.keys {
		if k.IdentityID == identityID && k.ID == keyID {
			if k.Status != "active" {
				return k.ProviderKeyID, true, nil
			}
			now := time.Now()
			k.Status = "revoked"
			k.RevokedAt = &now
			return k.ProviderKeyID, false, nil
		}
	}
	return "", false, signup.ErrNotFound
}

func (m *mockStore) RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*signup.KeyRecord, error) {
	// Revoke old (best-effort, mirrors real implementation) and inherit its
	// label/provenance, as the production SQL does.
	label, createdVia := "", signup.CreatedViaMagicLink
	for _, k := range m.keys {
		if k.IdentityID == identityID && k.ProviderKeyID == oldProviderKeyID && k.Status == "active" {
			now := time.Now()
			k.Status = "revoked"
			k.RevokedAt = &now
			label, createdVia = k.Label, k.CreatedVia
			break
		}
	}
	// Insert new — pass the caller's ctx so cancellation/deadlines propagate.
	return m.InsertKeyWithLabel(ctx, identityID, newProviderKeyID, label, createdVia)
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

func TestKey_MultipleActiveKeysUpToCap(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "grace@example.com", "", "")

	// Keys are per-agent now, so several may be active at once.
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		if _, err := store.InsertKey(ctx, id.ID, fmt.Sprintf("key_%d", i)); err != nil {
			t.Fatalf("insert key %d: %v", i, err)
		}
	}

	// One past the cap is refused.
	if _, err := store.InsertKey(ctx, id.ID, "one_too_many"); !errors.Is(err, signup.ErrActiveKeyLimitReached) {
		t.Errorf("expected ErrActiveKeyLimitReached, got %v", err)
	}

	keys, err := store.ListActiveKeys(ctx, id.ID)
	if err != nil {
		t.Fatalf("list active keys: %v", err)
	}
	if len(keys) != signup.MaxActiveKeysPerIdentity {
		t.Errorf("active keys = %d, want %d", len(keys), signup.MaxActiveKeysPerIdentity)
	}
}

func TestKey_RevokeByIDFreesCapSlot(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "heidi@example.com", "", "")
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		if _, err := store.InsertKey(ctx, id.ID, fmt.Sprintf("key_%d", i)); err != nil {
			t.Fatalf("insert key %d: %v", i, err)
		}
	}

	keys, _ := store.ListActiveKeys(ctx, id.ID)
	target := keys[0]

	providerKeyID, alreadyRevoked, err := store.RevokeKeyByID(ctx, id.ID, target.ID)
	if err != nil {
		t.Fatalf("revoke by id: %v", err)
	}
	if providerKeyID != target.ProviderKeyID {
		t.Errorf("provider key id = %q, want %q", providerKeyID, target.ProviderKeyID)
	}
	if alreadyRevoked {
		t.Error("first revoke should not report alreadyRevoked")
	}

	// A repeat revoke succeeds and reports alreadyRevoked, so the caller can
	// retry an upstream revocation that failed the first time.
	providerKeyID, alreadyRevoked, err = store.RevokeKeyByID(ctx, id.ID, target.ID)
	if err != nil {
		t.Fatalf("repeat revoke must be idempotent, got %v", err)
	}
	if !alreadyRevoked {
		t.Error("repeat revoke should report alreadyRevoked")
	}
	if providerKeyID != target.ProviderKeyID {
		t.Errorf("repeat revoke provider key id = %q, want %q", providerKeyID, target.ProviderKeyID)
	}

	// Scoped by identity: another identity cannot revoke this key by ID.
	if _, _, err := store.RevokeKeyByID(ctx, "someone-else", keys[1].ID); !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("cross-identity revoke: expected ErrNotFound, got %v", err)
	}

	// The freed slot can be reused.
	if _, err := store.InsertKey(ctx, id.ID, "replacement"); err != nil {
		t.Errorf("insert after revoke: %v", err)
	}
}

func TestKey_LabelAndProvenanceRecorded(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "ivan2@example.com", "", "")
	k, err := store.InsertKeyWithLabel(ctx, id.ID, "prov-agent", "Claude Code", signup.CreatedViaAgent)
	if err != nil {
		t.Fatalf("insert labelled key: %v", err)
	}
	if k.Label != "Claude Code" {
		t.Errorf("label = %q, want %q", k.Label, "Claude Code")
	}
	if k.CreatedVia != signup.CreatedViaAgent {
		t.Errorf("created_via = %q, want %q", k.CreatedVia, signup.CreatedViaAgent)
	}

	keys, _ := store.ListActiveKeys(ctx, id.ID)
	if len(keys) != 1 || keys[0].Label != "Claude Code" {
		t.Errorf("listed keys did not preserve the label: %+v", keys)
	}
}

func TestKey_RegenerateInheritsLabel(t *testing.T) {
	store := newMock()
	ctx := context.Background()

	id, _ := store.UpsertIdentity(ctx, "judy@example.com", "", "")
	if _, err := store.InsertKeyWithLabel(ctx, id.ID, "old-prov", "Cursor", signup.CreatedViaAgent); err != nil {
		t.Fatalf("seed: %v", err)
	}

	k, err := store.RevokeAndInsertKey(ctx, id.ID, "old-prov", "new-prov")
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	// Rotating an agent's key must not relabel it as a magic-link key.
	if k.Label != "Cursor" || k.CreatedVia != signup.CreatedViaAgent {
		t.Errorf("regenerated key = {label:%q via:%q}, want {Cursor agent}", k.Label, k.CreatedVia)
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
