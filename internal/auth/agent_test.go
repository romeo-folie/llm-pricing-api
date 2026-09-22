package auth_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/auth"
	"llm-pricing-api/internal/signup"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// newAgentTestApp builds an app wired with both a KeyIssuer and an AbuseGuard,
// which the device-grant flow exercises together.
func newAgentTestApp(store auth.Store, issuer auth.KeyIssuer, guard auth.AbuseGuard) *fiber.App {
	return newAgentTestAppCfg(store, issuer, guard, testCfg)
}

func newAgentTestAppCfg(store auth.Store, issuer auth.KeyIssuer, guard auth.AbuseGuard, cfg auth.Config) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	h := auth.New(store, &mockMailer{}, issuer, guard, cfg, zerolog.Nop())
	auth.Register(app.Group("/auth"), h)
	auth.RegisterAgent(app.Group("/auth/agent"), h)
	return app
}

// startGrant runs the device endpoint and returns its decoded response.
func startGrant(t *testing.T, app *fiber.App, body string) map[string]any {
	t.Helper()
	resp := doRequest(t, app, "POST", "/auth/agent/device", body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("device grant status = %d, want 200", resp.StatusCode)
	}
	return bodyJSON(t, resp)
}

// ── POST /auth/agent/device ───────────────────────────────────────────────────

func TestDeviceGrant_HappyPath(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})

	b := startGrant(t, app, `{"client_name":"Claude Code","platform":"darwin"}`)

	for _, key := range []string{
		"device_code", "user_code", "verification_uri",
		"verification_uri_complete", "expires_in", "interval",
	} {
		if _, ok := b[key]; !ok {
			t.Errorf("response missing %q", key)
		}
	}

	if b["verification_uri"] != "https://example.com/activate" {
		t.Errorf("verification_uri = %v, want https://example.com/activate", b["verification_uri"])
	}

	userCode, _ := b["user_code"].(string)
	// The code is shown to a human, so it is hyphenated for legibility.
	if !strings.Contains(userCode, "-") {
		t.Errorf("user_code = %q, want a hyphenated display form", userCode)
	}
	// ...and it must still normalise back to a valid stored code.
	if !signup.ValidUserCode(signup.NormalizeUserCode(userCode)) {
		t.Errorf("user_code %q does not normalise to a valid code", userCode)
	}

	complete, _ := b["verification_uri_complete"].(string)
	if !strings.Contains(complete, "code=") {
		t.Errorf("verification_uri_complete = %q, want an embedded code", complete)
	}

	if got := b["interval"].(float64); got != signup.AgentPollInterval.Seconds() {
		t.Errorf("interval = %v, want %v", got, signup.AgentPollInterval.Seconds())
	}
	if got := b["expires_in"].(float64); got <= 0 {
		t.Errorf("expires_in = %v, want positive", got)
	}
}

// The device code is the credential that redeems the key: only its hash may be
// persisted, and it must never appear in the grant record.
func TestDeviceGrant_StoresOnlyDeviceCodeHash(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})

	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	raw, _ := b["device_code"].(string)
	if raw == "" {
		t.Fatal("device_code missing")
	}

	if len(store.grants) != 1 {
		t.Fatalf("grants stored = %d, want 1", len(store.grants))
	}
	g := store.grants[0]
	if g.DeviceCodeHash == raw {
		t.Error("store persisted the raw device code")
	}
	if g.DeviceCodeHash != signup.HashToken(raw) {
		t.Error("store did not persist sha256(device_code)")
	}
	if g.Status != "pending" {
		t.Errorf("new grant status = %q, want pending", g.Status)
	}
	if g.IdentityID != nil {
		t.Error("a pending grant must not be bound to an identity")
	}
}

func TestDeviceGrant_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing client_name", `{"platform":"darwin"}`, fiber.StatusBadRequest},
		{"blank client_name", `{"client_name":"   "}`, fiber.StatusBadRequest},
		{"overlong client_name", `{"client_name":"` + strings.Repeat("a", 65) + `"}`, fiber.StatusBadRequest},
		// An escape sequence here would be printed in the user's browser and in
		// the agent's terminal, so it is rejected rather than sanitised.
		{"control characters in client_name", `{"client_name":"evil\u001b[31m"}`, fiber.StatusBadRequest},
		{"control characters in platform", `{"client_name":"ok","platform":"dar\u0007win"}`, fiber.StatusBadRequest},
		{"overlong platform", `{"client_name":"ok","platform":"` + strings.Repeat("a", 33) + `"}`, fiber.StatusBadRequest},
		{"malformed json", `{`, fiber.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newAgentTestApp(newMockStore(), &mockIssuer{}, &stubGuard{})
			resp := doRequest(t, app, "POST", "/auth/agent/device", tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestDeviceGrant_SignupDisabled_Returns503(t *testing.T) {
	cfg := testCfg
	cfg.SignupEnabled = false
	app := newAgentTestAppCfg(newMockStore(), &mockIssuer{}, &stubGuard{}, cfg)

	resp := doRequest(t, app, "POST", "/auth/agent/device", `{"client_name":"Claude Code"}`)
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestDeviceGrant_RateLimited_Returns429(t *testing.T) {
	guard := &stubGuard{agentDeviceErr: signup.ErrRateLimited}
	app := newAgentTestApp(newMockStore(), &mockIssuer{}, guard)

	resp := doRequest(t, app, "POST", "/auth/agent/device", `{"client_name":"Claude Code"}`)
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
}

// ── GET /auth/agent/grant ─────────────────────────────────────────────────────

func TestGetGrant_NoSession_Returns401(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})

	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code=ACDF0001", "")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetGrant_HappyPath(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code","platform":"linux"}`)
	userCode, _ := b["user_code"].(string)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code="+userCode, "", cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := bodyJSON(t, resp)
	if got["client_name"] != "Claude Code" {
		t.Errorf("client_name = %v, want Claude Code", got["client_name"])
	}
	if got["platform"] != "linux" {
		t.Errorf("platform = %v, want linux", got["platform"])
	}
	if got["status"] != "pending" {
		t.Errorf("status = %v, want pending", got["status"])
	}
	// The approver must be able to see which account is being authorised.
	if got["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", got["email"])
	}
	if _, ok := got["expires_at"]; !ok {
		t.Error("expires_at missing")
	}
}

// A code typed without its hyphen, or in lower case, must still resolve.
func TestGetGrant_AcceptsUnformattedCode(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	startGrant(t, app, `{"client_name":"Claude Code"}`)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	raw := store.grants[0].UserCode // e.g. ACDF0001
	messy := strings.ToLower(raw[:4] + "-" + raw[4:])

	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code="+messy, "", cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 for a lower-case hyphenated code", resp.StatusCode)
	}
}

func TestGetGrant_InvalidAndUnknownCodes(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	// Wrong length is a malformed request.
	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code=SHORT", "", cookie)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("malformed code: status = %d, want 400", resp.StatusCode)
	}

	// Well-formed but unknown is a 404.
	resp = doRequest(t, app, "GET", "/auth/agent/grant?user_code=ACDF9999", "", cookie)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("unknown code: status = %d, want 404", resp.StatusCode)
	}
}

func TestGetGrant_ExpiredReportsExpired(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	startGrant(t, app, `{"client_name":"Claude Code"}`)
	store.grants[0].ExpiresAt = time.Now().Add(-time.Minute)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code=ACDF0001", "", cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "expired" {
		t.Errorf("status = %v, want expired", got)
	}
}

func TestGetGrant_AttemptLimited_Returns429(t *testing.T) {
	store := newMockStore()
	guard := &stubGuard{userCodeErr: signup.ErrUserCodeAttempts}
	app := newAgentTestApp(store, &mockIssuer{}, guard)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "GET", "/auth/agent/grant?user_code=ACDF0001", "", cookie)
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
}

// ── POST /auth/agent/approve ──────────────────────────────────────────────────

func TestApproveGrant_NoSession_Returns401(t *testing.T) {
	app := newAgentTestApp(newMockStore(), &mockIssuer{}, &stubGuard{})
	resp := doRequest(t, app, "POST", "/auth/agent/approve", `{"user_code":"ACDF0001","decision":"approve"}`)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestApproveGrant_ApproveBindsIdentity(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	startGrant(t, app, `{"client_name":"Claude Code"}`)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/agent/approve",
		`{"user_code":"ACDF0001","decision":"approve"}`, cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "approved" {
		t.Errorf("status = %v, want approved", got)
	}

	g := store.grants[0]
	if g.Status != "approved" {
		t.Fatalf("stored status = %q, want approved", g.Status)
	}
	if g.IdentityID == nil || *g.IdentityID != "id-alice" {
		t.Errorf("identity_id = %v, want id-alice (the approving session)", g.IdentityID)
	}
	if g.DecidedAt == nil {
		t.Error("decided_at was not stamped")
	}
}

// A denial must not record which account happened to be signed in.
func TestApproveGrant_DenyLeavesNoIdentity(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	startGrant(t, app, `{"client_name":"Claude Code"}`)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/agent/approve",
		`{"user_code":"ACDF0001","decision":"deny"}`, cookie)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "denied" {
		t.Errorf("status = %v, want denied", got)
	}

	g := store.grants[0]
	if g.Status != "denied" {
		t.Errorf("stored status = %q, want denied", g.Status)
	}
	if g.IdentityID != nil {
		t.Error("a denied grant must not carry an identity_id")
	}
}

func TestApproveGrant_InvalidRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad decision", `{"user_code":"ACDF0001","decision":"maybe"}`, fiber.StatusBadRequest},
		{"empty decision", `{"user_code":"ACDF0001"}`, fiber.StatusBadRequest},
		{"malformed code", `{"user_code":"NOPE","decision":"approve"}`, fiber.StatusBadRequest},
		{"unknown code", `{"user_code":"ACDF9999","decision":"approve"}`, fiber.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockStore()
			app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
			startGrant(t, app, `{"client_name":"Claude Code"}`)
			cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

			resp := doRequest(t, app, "POST", "/auth/agent/approve", tc.body, cookie)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestApproveGrant_Expired_Returns410(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	startGrant(t, app, `{"client_name":"Claude Code"}`)
	store.grants[0].ExpiresAt = time.Now().Add(-time.Minute)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")

	resp := doRequest(t, app, "POST", "/auth/agent/approve",
		`{"user_code":"ACDF0001","decision":"approve"}`, cookie)
	if resp.StatusCode != fiber.StatusGone {
		t.Fatalf("status = %d, want 410", resp.StatusCode)
	}
}

// ── POST /auth/agent/token ────────────────────────────────────────────────────

func TestRedeemGrant_PendingThenIssued(t *testing.T) {
	store := newMockStore()
	store.identities["alice@example.com"] = &signup.Identity{ID: "id-alice", Email: "alice@example.com"}
	issuer := &mockIssuer{createKeyID: "prov-agent-1", createKey: "llmr_agent_key"}
	app := newAgentTestApp(store, issuer, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)

	// Before approval the agent polls and waits.
	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("pending status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "pending" {
		t.Fatalf("status = %v, want pending", got)
	}

	// The user approves in the browser.
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	aresp := doRequest(t, app, "POST", "/auth/agent/approve",
		`{"user_code":"ACDF0001","decision":"approve"}`, cookie)
	if aresp.StatusCode != fiber.StatusOK {
		t.Fatalf("approve status = %d, want 200", aresp.StatusCode)
	}

	// The next poll collects the key.
	resp = doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("issued status = %d, want 200", resp.StatusCode)
	}
	got := bodyJSON(t, resp)
	if got["status"] != "issued" {
		t.Fatalf("status = %v, want issued", got["status"])
	}
	if got["api_key"] != "llmr_agent_key" {
		t.Errorf("api_key = %v, want llmr_agent_key", got["api_key"])
	}
	if got["identity_id"] != "id-alice" {
		t.Errorf("identity_id = %v, want id-alice", got["identity_id"])
	}
	if got["email"] != "alice@example.com" {
		t.Errorf("email = %v, want alice@example.com", got["email"])
	}
	if got["label"] != "Claude Code" {
		t.Errorf("label = %v, want Claude Code", got["label"])
	}

	// The registry row records the agent as the owner, so the key can be
	// attributed and revoked independently.
	if len(store.keys) != 1 {
		t.Fatalf("keys stored = %d, want 1", len(store.keys))
	}
	if store.keys[0].Label != "Claude Code" {
		t.Errorf("stored label = %q, want Claude Code", store.keys[0].Label)
	}
	if store.keys[0].CreatedVia != signup.CreatedViaAgent {
		t.Errorf("stored created_via = %q, want agent", store.keys[0].CreatedVia)
	}
}

// The plaintext is served exactly once; a second poll must not re-issue.
func TestRedeemGrant_SecondRedeemIsRefused(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	doRequest(t, app, "POST", "/auth/agent/approve", `{"user_code":"ACDF0001","decision":"approve"}`, cookie)

	first := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if got := bodyJSON(t, first)["status"]; got != "issued" {
		t.Fatalf("first redeem status = %v, want issued", got)
	}

	second := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if second.StatusCode != fiber.StatusOK {
		t.Fatalf("second redeem status = %d, want 200", second.StatusCode)
	}
	body := bodyJSON(t, second)
	if body["status"] != "redeemed" {
		t.Errorf("second redeem status = %v, want redeemed", body["status"])
	}
	if _, present := body["api_key"]; present {
		t.Error("second redeem must not return a key")
	}
	// And no second key was created.
	if len(store.keys) != 1 {
		t.Errorf("keys stored = %d, want 1", len(store.keys))
	}
}

func TestRedeemGrant_Denied(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	doRequest(t, app, "POST", "/auth/agent/approve", `{"user_code":"ACDF0001","decision":"deny"}`, cookie)

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "denied" {
		t.Errorf("status = %v, want denied", got)
	}
}

func TestRedeemGrant_Expired(t *testing.T) {
	store := newMockStore()
	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)
	store.grants[0].ExpiresAt = time.Now().Add(-time.Minute)

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "expired" {
		t.Errorf("status = %v, want expired", got)
	}
}

func TestRedeemGrant_UnknownDeviceCode_Returns400(t *testing.T) {
	app := newAgentTestApp(newMockStore(), &mockIssuer{}, &stubGuard{})
	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"never-existed"}`)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRedeemGrant_MissingDeviceCode_Returns400(t *testing.T) {
	app := newAgentTestApp(newMockStore(), &mockIssuer{}, &stubGuard{})
	resp := doRequest(t, app, "POST", "/auth/agent/token", `{}`)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// Polling faster than the advertised interval is a client bug, so it is
// answered with slow_down rather than an error status.
func TestRedeemGrant_TooFast_ReturnsSlowDown(t *testing.T) {
	store := newMockStore()
	guard := &stubGuard{agentPollErr: signup.ErrPollTooFast}
	app := newAgentTestApp(store, &mockIssuer{}, guard)
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := bodyJSON(t, resp)
	if body["status"] != "slow_down" {
		t.Errorf("status = %v, want slow_down", body["status"])
	}
	want := (2 * signup.AgentPollInterval).Seconds()
	if body["interval"] != want {
		t.Errorf("interval = %v, want %v", body["interval"], want)
	}
}

func TestRedeemGrant_ActiveKeyLimit_Returns409(t *testing.T) {
	store := newMockStore()
	store.identities["alice@example.com"] = &signup.Identity{ID: "id-alice", Email: "alice@example.com"}
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		store.keys = append(store.keys, &signup.KeyRecord{
			ID:            "existing-" + strconv.Itoa(i),
			IdentityID:    "id-alice",
			ProviderKeyID: "prov-existing-" + strconv.Itoa(i),
			Status:        "active",
			CreatedAt:     time.Now(),
		})
	}

	app := newAgentTestApp(store, &mockIssuer{}, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	doRequest(t, app, "POST", "/auth/agent/approve", `{"user_code":"ACDF0001","decision":"approve"}`, cookie)

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	// No key may be added beyond the cap.
	if len(store.keys) != signup.MaxActiveKeysPerIdentity {
		t.Errorf("keys = %d, want %d", len(store.keys), signup.MaxActiveKeysPerIdentity)
	}
	// The grant is released so the agent can retry once the user frees a slot.
	if store.grants[0].Status != "approved" {
		t.Errorf("grant status = %q, want approved (released for retry)", store.grants[0].Status)
	}
}

// A transient issuer failure must not strand the agent with a consumed grant.
func TestRedeemGrant_IssuanceFailure_IsRetryable(t *testing.T) {
	store := newMockStore()
	issuer := &mockIssuer{createErr: errors.New("unkey unavailable")}
	app := newAgentTestApp(store, issuer, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	doRequest(t, app, "POST", "/auth/agent/approve", `{"user_code":"ACDF0001","decision":"approve"}`, cookie)

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	// The claim was reverted, so the agent is not stuck.
	if store.grants[0].Status != "approved" {
		t.Fatalf("grant status = %q, want approved after revert", store.grants[0].Status)
	}
	if store.grants[0].RedeemedAt != nil {
		t.Error("redeemed_at should be cleared by the revert")
	}

	// The retry succeeds.
	issuer.createErr = nil
	resp = doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("retry status = %d, want 200", resp.StatusCode)
	}
	if got := bodyJSON(t, resp)["status"]; got != "issued" {
		t.Errorf("retry status = %v, want issued", got)
	}
}

// A registry write failure must revoke the Unkey key it could not record,
// otherwise a usable key exists that no registry row knows about.
func TestRedeemGrant_PersistFailure_RevokesOrphanUnkeyKey(t *testing.T) {
	store := newMockStore()
	var revoked []string
	issuer := &mockIssuer{createKeyID: "orphan-prov", createKey: "llmr_orphan"}
	issuer.revokeHook = func(providerKeyID string) { revoked = append(revoked, providerKeyID) }

	app := newAgentTestApp(store, issuer, &stubGuard{})
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)
	deviceCode, _ := b["device_code"].(string)
	cookie := makeSessionCookie(t, "id-alice", "alice@example.com")
	doRequest(t, app, "POST", "/auth/agent/approve", `{"user_code":"ACDF0001","decision":"approve"}`, cookie)

	// Fail the registry write after Unkey has already minted the key.
	store.insertKeyErr = errors.New("registry unavailable")

	resp := doRequest(t, app, "POST", "/auth/agent/token", `{"device_code":"`+deviceCode+`"}`)
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	found := false
	for _, id := range revoked {
		if id == "orphan-prov" {
			found = true
		}
	}
	if !found {
		t.Errorf("Unkey key %q was not revoked after the registry write failed", "orphan-prov")
	}
	// The grant is released so the agent can retry.
	if store.grants[0].Status != "approved" {
		t.Errorf("grant status = %q, want approved after revert", store.grants[0].Status)
	}
}

// A misconfigured grant TTL must be clamped rather than honoured. An absurd
// value is either a typo or an attempt to mint a near-permanent authorization,
// and letting the multiplication overflow would produce a negative duration and
// an instantly-expired grant.
func TestDeviceGrant_TTLIsClamped(t *testing.T) {
	store := newMockStore()
	cfg := testCfg
	cfg.AgentGrantTTLMinutes = 100000 // ~69 days, far past the clamp

	app := newAgentTestAppCfg(store, &mockIssuer{}, &stubGuard{}, cfg)
	b := startGrant(t, app, `{"client_name":"Claude Code"}`)

	expiresIn, ok := b["expires_in"].(float64)
	if !ok {
		t.Fatalf("expires_in missing or not a number: %v", b["expires_in"])
	}
	if expiresIn <= 0 {
		t.Fatalf("expires_in = %v, want a positive duration (overflow would make it negative)", expiresIn)
	}
	if expiresIn > 3600 {
		t.Errorf("expires_in = %v, want it clamped to at most 3600s", expiresIn)
	}

	// And the stored grant must expire in the same window the client was told.
	if len(store.grants) != 1 {
		t.Fatalf("grants stored = %d, want 1", len(store.grants))
	}
	remaining := time.Until(store.grants[0].ExpiresAt)
	if remaining <= 0 || remaining > time.Hour+time.Minute {
		t.Errorf("stored expiry is %v away, want within the clamped hour", remaining)
	}
}
