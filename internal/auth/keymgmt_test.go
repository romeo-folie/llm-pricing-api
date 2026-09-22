package auth_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/signup"
)

// seedKey adds an active key directly to the mock store.
func seedKey(store *mockStore, identityID, keyID, providerKeyID, label, createdVia string) {
	store.keys = append(store.keys, &signup.KeyRecord{
		ID:            keyID,
		IdentityID:    identityID,
		ProviderKeyID: providerKeyID,
		Label:         label,
		CreatedVia:    createdVia,
		Status:        "active",
		CreatedAt:     time.Now(),
	})
}

func seedIdentity(store *mockStore, id, email string) {
	store.identities[email] = &signup.Identity{ID: id, Email: email}
}

// ── GET /auth/signup/keys ─────────────────────────────────────────────────────

func TestListKeys_NoSession_Returns401(t *testing.T) {
	app := newTestApp(newMockStore(), &mockMailer{})
	resp := doRequest(t, app, "GET", "/auth/signup/keys", "")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// The list must never include a secret: a plaintext exists only in the response
// that created it, and provider_key_id is an internal Unkey identifier.
func TestListKeys_ReturnsMetadataWithoutSecrets(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "Claude Code", signup.CreatedViaAgent)
	seedKey(store, "id-alice", "k2", "prov-2", "", signup.CreatedViaMagicLink)

	app := newTestApp(store, &mockMailer{})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "GET", "/auth/signup/keys", "", cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b := bodyJSON(t, resp)
	if b["count"] != float64(2) {
		t.Errorf("count = %v, want 2", b["count"])
	}
	if b["max_keys"] != float64(signup.MaxActiveKeysPerIdentity) {
		t.Errorf("max_keys = %v, want %d", b["max_keys"], signup.MaxActiveKeysPerIdentity)
	}

	keys, ok := b["keys"].([]any)
	if !ok || len(keys) != 2 {
		t.Fatalf("keys = %v, want a 2-element array", b["keys"])
	}
	for _, raw := range keys {
		k := raw.(map[string]any)
		if _, present := k["provider_key_id"]; present {
			t.Error("provider_key_id must not be exposed")
		}
		if _, present := k["plaintext"]; present {
			t.Error("plaintext must never appear in the key list")
		}
		if _, present := k["label"]; !present {
			t.Error("label missing from key entry")
		}
		if _, present := k["created_via"]; !present {
			t.Error("created_via missing from key entry")
		}
	}
}

func TestListKeys_ExcludesRevokedAndOtherIdentities(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "", signup.CreatedViaMagicLink)
	seedKey(store, "id-bob", "k2", "prov-2", "", signup.CreatedViaMagicLink)
	// A revoked key of the same identity must not be listed.
	seedKey(store, "id-alice", "k3", "prov-3", "", signup.CreatedViaMagicLink)
	store.keys[2].Status = "revoked"

	app := newTestApp(store, &mockMailer{})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	b := bodyJSON(t, doRequest(t, app, "GET", "/auth/signup/keys", "", cookie))
	if b["count"] != float64(1) {
		t.Errorf("count = %v, want 1 (only alice's active key)", b["count"])
	}
}

// ── DELETE /auth/signup/keys/:id ──────────────────────────────────────────────

func TestRevokeKey_NoSession_Returns401(t *testing.T) {
	app := newTestApp(newMockStore(), &mockMailer{})
	resp := doRequest(t, app, "DELETE", "/auth/signup/keys/k1", "")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRevokeKey_RevokesRegistryAndUnkey(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "Claude Code", signup.CreatedViaAgent)

	var revoked []string
	issuer := &mockIssuer{}
	issuer.revokeHook = func(providerKeyID string) { revoked = append(revoked, providerKeyID) }

	app := newTestAppWithIssuer(store, &mockMailer{}, issuer)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "DELETE", "/auth/signup/keys/k1", "", cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if store.keys[0].Status != "revoked" {
		t.Errorf("registry status = %q, want revoked", store.keys[0].Status)
	}
	if store.keys[0].RevokedAt == nil {
		t.Error("revoked_at was not stamped")
	}
	// The upstream key must be revoked too, or it keeps authenticating.
	if len(revoked) != 1 || revoked[0] != "prov-1" {
		t.Errorf("Unkey RevokeKey calls = %v, want [prov-1]", revoked)
	}
}

func TestRevokeKey_UnknownOrCrossIdentity_Returns404(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-bob", "bobs-key", "prov-bob", "", signup.CreatedViaMagicLink)

	app := newTestApp(store, &mockMailer{})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	// Unknown ID.
	resp := doRequest(t, app, "DELETE", "/auth/signup/keys/nope", "", cookie)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("unknown key: status = %d, want 404", resp.StatusCode)
	}
	// Another identity's key: indistinguishable from unknown, and untouched.
	resp = doRequest(t, app, "DELETE", "/auth/signup/keys/bobs-key", "", cookie)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("cross-identity key: status = %d, want 404", resp.StatusCode)
	}
	if store.keys[0].Status != "active" {
		t.Error("another identity's key was revoked")
	}
}

// ── POST /auth/signup/regenerate-key with a selected key ──────────────────────

// Regenerating with no explicit key_id must target the browser flow's own key.
// Rotating whichever key happened to be first would silently break an agent.
func TestRegenerateKey_DefaultTargetsMagicLinkKeyOnly(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k-agent", "prov-agent", "Claude Code", signup.CreatedViaAgent)
	seedKey(store, "id-alice", "k-browser", "prov-browser", "", signup.CreatedViaMagicLink)

	app := newTestAppWithIssuer(store, &mockMailer{}, &mockIssuer{createKeyID: "new", createKey: "llmr_new"})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", "", cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 (browser flow regenerates its own key)", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["plaintext"]; got != "llmr_new" {
		t.Errorf("plaintext = %v, want llmr_new", got)
	}

	for _, k := range store.keys {
		switch k.ID {
		case "k-browser":
			if k.Status != "revoked" {
				t.Errorf("magic-link key status = %q, want revoked", k.Status)
			}
		case "k-agent":
			if k.Status != "active" {
				t.Errorf("agent key status = %q, want active (must not be rotated implicitly)", k.Status)
			}
		}
	}
}

// With every slot held by an agent key there is nothing safe to rotate and no
// room to mint, so the caller is told to make an explicit choice instead of the
// server silently breaking an agent.
func TestRegenerateKey_AllAgentKeysAtCap_Returns409(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		seedKey(store, "id-alice", fmt.Sprintf("k%d", i), fmt.Sprintf("prov-%d", i), "Claude Code", signup.CreatedViaAgent)
	}

	app := newTestAppWithIssuer(store, &mockMailer{}, &mockIssuer{createKeyID: "new", createKey: "llmr_new"})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", "", cookie)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	// Nothing may have been revoked or minted.
	for _, k := range store.keys {
		if k.Status != "active" {
			t.Errorf("key %s status = %q, want active", k.ID, k.Status)
		}
	}
	if len(store.keys) != signup.MaxActiveKeysPerIdentity {
		t.Errorf("keys = %d, want %d", len(store.keys), signup.MaxActiveKeysPerIdentity)
	}
}

func TestRegenerateKey_TargetsSpecifiedKey(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "Claude Code", signup.CreatedViaAgent)
	seedKey(store, "id-alice", "k2", "prov-2", "", signup.CreatedViaMagicLink)

	app := newTestAppWithIssuer(store, &mockMailer{}, &mockIssuer{createKeyID: "new-prov", createKey: "llmr_new"})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", `{"key_id":"k2"}`, cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b := bodyJSON(t, resp)
	if b["plaintext"] != "llmr_new" {
		t.Errorf("plaintext = %v, want llmr_new", b["plaintext"])
	}

	// The targeted key is gone; the other agent's key is untouched.
	if store.keys[1].Status != "revoked" {
		t.Errorf("targeted key status = %q, want revoked", store.keys[1].Status)
	}
	if store.keys[0].Status != "active" {
		t.Errorf("untargeted key status = %q, want active", store.keys[0].Status)
	}
	// The replacement inherits the replaced key's provenance.
	if store.keys[2].ID == "" || store.keys[2].Status != "active" {
		t.Errorf("replacement key not inserted: %+v", store.keys[2])
	}
}

func TestRegenerateKey_UnknownKeyID_Returns404(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "", signup.CreatedViaMagicLink)

	app := newTestAppWithIssuer(store, &mockMailer{}, &mockIssuer{})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/signup/regenerate-key", `{"key_id":"nope"}`, cookie)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// ── /me key accounting ────────────────────────────────────────────────────────

func TestMe_ReportsKeyCountAndCap(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "Claude Code", signup.CreatedViaAgent)

	app := newTestApp(store, &mockMailer{})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	b := bodyJSON(t, doRequest(t, app, "GET", "/auth/signup/me", "", cookie))
	if b["has_active_key"] != true {
		t.Errorf("has_active_key = %v, want true", b["has_active_key"])
	}
	if b["key_count"] != float64(1) {
		t.Errorf("key_count = %v, want 1", b["key_count"])
	}
	if b["max_keys"] != float64(signup.MaxActiveKeysPerIdentity) {
		t.Errorf("max_keys = %v, want %d", b["max_keys"], signup.MaxActiveKeysPerIdentity)
	}
}

// ── Issue key at the cap ──────────────────────────────────────────────────────

// The reveal-once guard normally short-circuits issue-key before the cap can be
// hit, so this exercises the cap mapping through a concurrent-insert race:
// no keys listed, but the insert reports the limit.
func TestIssueKey_LimitRace_Returns409(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	store.insertKeyErr = signup.ErrActiveKeyLimitReached

	var revoked []string
	issuer := &mockIssuer{}
	issuer.revokeHook = func(providerKeyID string) { revoked = append(revoked, providerKeyID) }

	app := newTestAppWithIssuer(store, &mockMailer{}, issuer)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/signup/issue-key", "", cookie)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	// The key Unkey minted has no registry row, so it must have been revoked.
	if len(revoked) != 1 {
		t.Errorf("Unkey revocations = %v, want exactly one", revoked)
	}
}

// ── Signup-disabled gating for the token endpoint ─────────────────────────────

func TestRedeemGrant_SignupDisabled_Returns503(t *testing.T) {
	cfg := testCfg
	cfg.SignupEnabled = false
	app := newAgentTestAppCfg(newMockStore(), &mockIssuer{}, &stubGuard{}, cfg)

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"anything"}`)
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// ── Revocation honesty ───────────────────────────────────────────────────────

// A registry-only revocation is not a revocation: /v1 authorises against Unkey,
// so the credential keeps working. Reporting 200 here would tell a user their
// leaked key is dead when it is still live, and nothing would ever retry it.
func TestRevokeKey_UpstreamFailure_Returns502AndIsRetriable(t *testing.T) {
	store := newMockStore()
	seedIdentity(store, "id-alice", "alice@example.com")
	seedKey(store, "id-alice", "k1", "prov-1", "Claude Code", signup.CreatedViaAgent)

	issuer := &mockIssuer{revokeErr: errors.New("unkey unavailable")}
	app := newTestAppWithIssuer(store, &mockMailer{}, issuer)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "DELETE", "/auth/signup/keys/k1", "", cookie)
	if resp.StatusCode != fiber.StatusBadGateway {
		t.Fatalf("status = %d, want 502 while the key is still live upstream", resp.StatusCode)
	}
	// The local row is revoked anyway, because that part is idempotent. It is
	// what makes the retry below able to finish the job.
	if store.keys[0].Status != "revoked" {
		t.Errorf("registry status = %q, want revoked", store.keys[0].Status)
	}

	// Once upstream recovers, retrying succeeds and says so.
	issuer.revokeErr = nil
	retry := doRequest(t, app, "DELETE", "/auth/signup/keys/k1", "", cookie)
	if retry.StatusCode != fiber.StatusOK {
		t.Fatalf("retry status = %d, want 200", retry.StatusCode)
	}
	if got := bodyJSON(t, retry)["already_revoked"]; got != true {
		t.Errorf("already_revoked = %v, want true", got)
	}
}

// The per-code counter is a brake on one code, not an enumeration defence. The
// per-caller counter is what bounds probing, so a burst of guesses from one
// caller must be refused even though each code is fresh.
func TestGetGrant_PerCallerProbeLimit_Returns429(t *testing.T) {
	store := newMockStore()
	guard := &stubGuard{codeProbeErr: signup.ErrRateLimited}
	app := newAgentTestApp(store, &mockIssuer{}, guard)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code=ACDF0001", "", cookie)
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
}
