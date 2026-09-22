// Package auth — agent.go implements the device-authorization flow that lets an
// agent obtain an API key on behalf of its user (RFC 8628 shape).
//
//	POST /auth/agent/device   — agent starts a grant, receives a device and user code
//	GET  /auth/agent/grant    — browser: what am I being asked to approve?
//	POST /auth/agent/approve  — browser: approve or deny
//	POST /auth/agent/token    — agent: poll for status, then collect the key once
//
// The point of the flow is where the credential lands. The magic-link flow
// delivers a key to a browser, which forces the user to move it by hand into the
// agent's config — through the clipboard, a terminal, or worse, a chat
// transcript. Here the key travels server → agent over the polling back
// channel and never passes through the user at all. The user's only action is a
// decision.
//
// Two codes exist per grant and they are not interchangeable:
//
//   - device_code is the bearer credential for collecting the key. It is
//     returned once, stored only as a SHA-256 hash, and must never be logged.
//   - user_code is what the human types or clicks. It is deliberately useless
//     on its own: knowing it lets you approve into your own identity, not read
//     someone else's key.
//
// The token endpoint answers with 200 + a status field for every expected
// outcome (pending, slow_down, issued, denied, expired, redeemed) and reserves
// problem+json for genuinely malformed or refused requests. An agent polling
// every 5s for 10 minutes must not generate ~120 4xx responses that pollute
// error-rate metrics and trip the API's own alerts.
package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/logger"
	"llm-pricing-api/internal/middleware"
	"llm-pricing-api/internal/signup"
)

// agentActivatePath is the frontend route that renders the approval screen.
// It is a page, not an API route: the user approves in a browser they are
// already signed in to.
const agentActivatePath = "/activate"

// slowDownInterval is the interval advertised when a caller polls too fast.
// Twice the normal interval, per RFC 8628 §3.5.
const slowDownInterval = 2 * signup.AgentPollInterval

// ── POST /auth/agent/device ──────────────────────────────────────────────────

type deviceGrantBody struct {
	ClientName string `json:"client_name"`
	Platform   string `json:"platform"`
}

// DeviceGrant starts a device-authorization grant. Unauthenticated by design:
// the caller has no key yet, which is the entire reason it is here.
//
// The response contains the raw device code exactly once. It is never stored in
// plaintext and never logged.
func (h *Handler) DeviceGrant(c *fiber.Ctx) error {
	if !h.cfg.SignupEnabled {
		return api.NewServiceUnavailable("signup is currently disabled")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	var body deviceGrantBody
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid request body")
	}
	clientName := strings.TrimSpace(body.ClientName)
	platform := strings.TrimSpace(body.Platform)
	if err := signup.ValidateAgentClientName(clientName); err != nil {
		return api.NewBadRequest(err.Error())
	}
	if err := signup.ValidateAgentPlatform(platform); err != nil {
		return api.NewBadRequest(err.Error())
	}

	if h.guard != nil {
		switch err := h.guard.CheckAgentDevice(c.UserContext(), middleware.RealIP(c)); {
		case err == nil:
			// allowed
		case errors.Is(err, signup.ErrRateLimited):
			return api.NewTooManyRequests(err.Error())
		default:
			log.Error().Err(err).Msg("auth: agent-device guard error — allowing request")
		}
	}

	ttl := h.cfg.agentGrantTTL()
	grant, rawDeviceCode, err := h.store.CreateAgentGrant(
		c.UserContext(), clientName, platform, time.Now().Add(ttl),
	)
	if err != nil {
		log.Error().Err(err).Msg("auth: create agent grant failed")
		return api.NewInternalError("could not start authorization")
	}

	verificationURI, verificationURIComplete := h.activateURLs(grant.UserCode)

	// Note what is absent from this response: no identity, and no key. Nothing
	// is issued until a signed-in human approves.
	return c.JSON(fiber.Map{
		"device_code":               rawDeviceCode,
		"user_code":                 signup.FormatUserCode(grant.UserCode),
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURIComplete,
		"expires_in":                int(ttl.Seconds()),
		"interval":                  int(signup.AgentPollInterval.Seconds()),
	})
}

// ── GET /auth/agent/grant ────────────────────────────────────────────────────

// GetGrant describes a pending grant to the signed-in user who is deciding on
// it. RequireSession runs first, so an unauthenticated caller learns nothing
// about whether a code exists — code-space probing requires an account.
func (h *Handler) GetGrant(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	userCode := signup.NormalizeUserCode(c.Query("user_code"))
	if !signup.ValidUserCode(userCode) {
		return api.NewBadRequest("invalid user code")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	if err := h.guardCodeAttempt(c, userCode); err != nil {
		return err
	}

	grant, err := h.store.GetAgentGrantByUserCode(c.UserContext(), userCode)
	if err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return api.NewNotFound("that code was not found or has expired")
		}
		log.Error().Err(err).Msg("auth: load agent grant failed")
		return api.NewInternalError("could not load request")
	}

	return c.JSON(fiber.Map{
		"user_code":   signup.FormatUserCode(grant.UserCode),
		"client_name": grant.ClientName,
		"platform":    grant.Platform,
		"status":      grantDisplayStatus(grant),
		"expires_at":  grant.ExpiresAt,
		// Echoed so the approval screen can state which account is being
		// authorised — the user needs to recognise it before consenting.
		"email": session.Email,
	})
}

// ── POST /auth/agent/approve ─────────────────────────────────────────────────

type approveGrantBody struct {
	UserCode string `json:"user_code"`
	Decision string `json:"decision"`
}

// ApproveGrant records the signed-in user's decision on a grant.
// RequireSession runs first; the decision is bound to the session identity, so
// a user can only ever authorise a key for their own account.
func (h *Handler) ApproveGrant(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	var body approveGrantBody
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid request body")
	}

	userCode := signup.NormalizeUserCode(body.UserCode)
	if !signup.ValidUserCode(userCode) {
		return api.NewBadRequest("invalid user code")
	}

	var approve bool
	switch strings.ToLower(strings.TrimSpace(body.Decision)) {
	case "approve", "approved":
		approve = true
	case "deny", "denied":
		approve = false
	default:
		return api.NewBadRequest("decision must be approve or deny")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	if err := h.guardCodeAttempt(c, userCode); err != nil {
		return err
	}

	grant, err := h.store.DecideAgentGrant(c.UserContext(), userCode, session.IdentityID, approve)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrNotFound):
			// Deliberately one message for unknown, already-decided and
			// already-redeemed codes: distinguishing them would confirm which
			// codes exist.
			return api.NewNotFound("that code was not found, has expired, or was already used")
		case errors.Is(err, signup.ErrGrantExpired):
			return api.NewGone("that code has expired — ask the agent to start again")
		case errors.Is(err, signup.ErrGrantRedeemed):
			return api.NewGone("that code has already been used")
		default:
			log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: decide agent grant failed")
			return api.NewInternalError("could not record your decision")
		}
	}

	status := "denied"
	if approve {
		status = "approved"
	}
	log.Info().
		Str("grant_id", grant.ID).
		Str("client_name", truncate(grant.ClientName, 64)).
		Str("decision", status).
		Msg("auth: agent grant decided")

	return c.JSON(fiber.Map{
		"status":      status,
		"client_name": grant.ClientName,
	})
}

// ── POST /auth/agent/token ───────────────────────────────────────────────────

type redeemGrantBody struct {
	DeviceCode string `json:"device_code"`
}

// RedeemGrant is the agent's polling endpoint. It reports the grant's state and,
// on the single successful poll, issues the API key.
//
// The key is issued only after the grant has been atomically claimed, and the
// claim is reverted if issuance fails, so a transient upstream error does not
// strand the agent with a consumed grant and no key.
func (h *Handler) RedeemGrant(c *fiber.Ctx) error {
	if !h.cfg.SignupEnabled {
		return api.NewServiceUnavailable("signup is currently disabled")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	var body redeemGrantBody
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid request body")
	}
	rawDeviceCode := strings.TrimSpace(body.DeviceCode)
	if rawDeviceCode == "" {
		return api.NewBadRequest("device_code is required")
	}

	// Throttle before any database work. Polling faster than `interval` is a
	// client bug, not an attack, so it is answered with slow_down rather than
	// an error.
	if h.guard != nil {
		if err := h.guard.CheckAgentPoll(c.UserContext(), rawDeviceCode); err != nil {
			if errors.Is(err, signup.ErrPollTooFast) {
				return c.JSON(fiber.Map{
					"status":   "slow_down",
					"interval": int(slowDownInterval.Seconds()),
				})
			}
			log.Error().Err(err).Msg("auth: agent-poll guard error — allowing request")
		}
	}

	// Read first so non-redeemable states are reported without consuming the
	// grant. The authoritative claim is RedeemAgentGrant below; this read is
	// only for deciding which status to report.
	existing, err := h.store.GetAgentGrantByDeviceCode(c.UserContext(), rawDeviceCode)
	if err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			// An unknown code is the agent's own bug (lost state, retyped), so
			// it is a real 400 rather than a pollable status.
			return api.NewBadRequest("unknown device_code")
		}
		log.Error().Err(err).Msg("auth: load agent grant failed")
		return api.NewInternalError("could not read authorization")
	}

	if !existing.ExpiresAt.After(time.Now()) {
		return c.JSON(fiber.Map{"status": "expired", "message": "authorization expired — start a new request"})
	}
	switch existing.Status {
	case "pending":
		return c.JSON(fiber.Map{"status": "pending", "interval": int(signup.AgentPollInterval.Seconds())})
	case "denied":
		return c.JSON(fiber.Map{"status": "denied", "message": "the user denied this request"})
	case "redeemed":
		// Not necessarily terminal. This poller may have caught the grant while a
		// concurrent poller held the claim inside its key-issuance call; if that
		// issuance then failed, the claim is reverted and the grant is redeemable
		// again. Reporting the terminal "redeemed" here would make the agent
		// abandon a live grant, which is the stranding the revert exists to
		// prevent. Re-read once before committing to that answer.
		if current, readErr := h.store.GetAgentGrantByDeviceCode(c.UserContext(), rawDeviceCode); readErr == nil && current.Status == "approved" {
			return c.JSON(fiber.Map{
				"status":   "pending",
				"interval": int(signup.AgentPollInterval.Seconds()),
			})
		}
		return c.JSON(fiber.Map{"status": "redeemed", "message": "this authorization was already used — start a new request"})
	}

	// Status is 'approved' (or about to expire): claim it. Exactly one
	// concurrent poller can win this transition, so the plaintext is served once.
	grant, err := h.store.RedeemAgentGrant(c.UserContext(), rawDeviceCode)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrGrantPending):
			return c.JSON(fiber.Map{"status": "pending", "interval": int(signup.AgentPollInterval.Seconds())})
		case errors.Is(err, signup.ErrGrantDenied):
			return c.JSON(fiber.Map{"status": "denied"})
		case errors.Is(err, signup.ErrGrantExpired):
			return c.JSON(fiber.Map{"status": "expired"})
		case errors.Is(err, signup.ErrGrantRedeemed):
			// This poller lost a race to a concurrent one. That loser cannot tell
			// from the error alone whether the winner delivered the key or
			// reverted its claim after a failed issuance — and reporting the
			// terminal "redeemed" in the second case makes the agent abandon a
			// grant that is still redeemable. Re-read to tell the two apart.
			if current, readErr := h.store.GetAgentGrantByDeviceCode(c.UserContext(), rawDeviceCode); readErr == nil && current.Status == "approved" {
				return c.JSON(fiber.Map{
					"status":   "pending",
					"interval": int(signup.AgentPollInterval.Seconds()),
				})
			}
			return c.JSON(fiber.Map{"status": "redeemed", "message": "this authorization was already used"})
		default:
			log.Error().Err(err).Msg("auth: redeem agent grant failed")
			return api.NewInternalError("could not complete authorization")
		}
	}

	if grant.IdentityID == nil || *grant.IdentityID == "" {
		// Unreachable: the DB CHECK requires an identity on approved grants.
		log.Error().Str("grant_id", grant.ID).Msg("auth: approved grant has no identity_id")
		h.revertGrant(c.UserContext(), grant.ID)
		return api.NewInternalError("could not complete authorization")
	}
	identityID := *grant.IdentityID

	// Pre-check the cap so a user already at the limit does not cost a wasted
	// Unkey create/revoke pair on every poll. The authoritative enforcement is
	// inside InsertKeyWithLabel below, which is race-free.
	keys, err := h.store.ListActiveKeys(c.UserContext(), identityID)
	if err != nil {
		log.Error().Err(err).Str("identity_id", identityID).Msg("auth: list keys during redemption failed")
		h.revertGrant(c.UserContext(), grant.ID)
		return api.NewInternalError("could not complete authorization")
	}
	if len(keys) >= signup.MaxActiveKeysPerIdentity {
		h.revertGrant(c.UserContext(), grant.ID)
		return api.NewConflict("this account has reached its active key limit — revoke a key and try again")
	}

	providerKeyID, plaintext, err := h.issuer.CreateKey(c.UserContext(), "", identityID) // apiID omitted — issuer uses its stored value
	if err != nil {
		log.Error().Err(err).Str("identity_id", identityID).Msg("auth: create Unkey key for agent grant failed")
		h.revertGrant(c.UserContext(), grant.ID)
		return api.NewInternalError("could not issue API key")
	}

	if _, err := h.store.InsertKeyWithLabel(
		c.UserContext(), identityID, providerKeyID, grant.ClientName, signup.CreatedViaAgent,
	); err != nil {
		// The Unkey key exists but has no registry row — revoke it so it cannot
		// authenticate. Then let the agent retry. The revocation is logged rather
		// than discarded: if it fails, a usable key is left live with nothing
		// referencing it, which is exactly the case an operator must be able to
		// find in the logs.
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.UserContext()), 10*time.Second)
		if revokeErr := h.issuer.RevokeKey(revokeCtx, providerKeyID); revokeErr != nil {
			log.Error().Err(revokeErr).Str("provider_key_id", providerKeyID).Str("grant_id", grant.ID).
				Msg("auth: failed to revoke orphaned key after registry write failed — key has no registry row and may still authenticate")
		}
		cancel()
		if errors.Is(err, signup.ErrActiveKeyLimitReached) {
			h.revertGrant(c.UserContext(), grant.ID)
			return api.NewConflict("this account has reached its active key limit — revoke a key and try again")
		}
		log.Error().Err(err).Str("identity_id", identityID).Msg("auth: insert agent key record failed")
		h.revertGrant(c.UserContext(), grant.ID)
		return api.NewInternalError("could not persist API key")
	}

	log.Info().
		Str("grant_id", grant.ID).
		Str("identity_id", identityID).
		Str("client_name", truncate(grant.ClientName, 64)).
		Msg("auth: agent grant redeemed and key issued")

	// Best-effort: the agent needs to know which account it is acting for, but
	// a lookup failure must not undo a successful issuance.
	var email string
	if ident, identErr := h.store.GetIdentityByID(c.UserContext(), identityID); identErr == nil {
		email = ident.Email
	} else {
		log.Warn().Err(identErr).Str("identity_id", identityID).Msg("auth: identity lookup after issuance failed — omitting email")
	}

	// This is the one and only response carrying the plaintext.
	return c.JSON(fiber.Map{
		"status":      "issued",
		"api_key":     plaintext,
		"identity_id": identityID,
		"email":       email,
		"label":       grant.ClientName,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// activateURLs builds the base activation URL and the one-click variant with the
// user code embedded. net/url is used so the code is encoded safely.
func (h *Handler) activateURLs(userCode string) (string, string) {
	base := strings.TrimRight(h.cfg.MagicLinkBaseURL, "/")
	u, err := url.Parse(base)
	if err != nil {
		// Fallback: should not happen with validated config.
		plain := base + agentActivatePath
		return plain, plain + "?code=" + url.QueryEscape(signup.FormatUserCode(userCode))
	}
	if joined, jerr := url.JoinPath(u.Path, agentActivatePath); jerr == nil {
		u.Path = joined
	} else {
		u.Path = u.Path + agentActivatePath
	}
	plain := u.String()
	q := u.Query()
	// The display form (with hyphen) is used deliberately: a user may read the
	// URL aloud or type it, and the API normalises the hyphen away on input.
	q.Set("code", signup.FormatUserCode(userCode))
	u.RawQuery = q.Encode()
	return plain, u.String()
}

// guardCodeAttempt applies the per-caller and per-code attempt limits.
// Returns a problem detail when a limit is exceeded, nil otherwise.
func (h *Handler) guardCodeAttempt(c *fiber.Ctx, userCode string) error {
	if h.guard == nil {
		return nil
	}

	// Per-caller bound first. This is the control that actually bounds code-space
	// enumeration: an attacker trying codes tries a different one each time, and
	// each fresh code starts its own per-code counter. It also stops one caller
	// burning a known code's budget to lock the legitimate approver out.
	if err := h.guard.CheckCodeProbe(c.UserContext(), middleware.RealIP(c)); err != nil {
		if errors.Is(err, signup.ErrRateLimited) {
			return api.NewTooManyRequests(err.Error())
		}
		log := logger.FromContext(c.UserContext(), h.log)
		log.Error().Err(err).Msg("auth: code-probe guard error — allowing request")
	}

	err := h.guard.CheckUserCodeAttempt(c.UserContext(), userCode)
	if err == nil {
		return nil
	}
	if errors.Is(err, signup.ErrUserCodeAttempts) {
		return api.NewTooManyRequests(err.Error())
	}
	log := logger.FromContext(c.UserContext(), h.log)
	log.Error().Err(err).Msg("auth: user-code guard error — allowing request")
	return nil
}

// revertGrant returns a claimed grant to the approved state so the agent can
// retry. Runs on a detached context because the request context may already be
// cancelled, and logs rather than propagates failure: every caller is already
// returning an error, and a stranded grant is an operator-visible problem, not
// a second response field.
func (h *Handler) revertGrant(ctx context.Context, grantID string) {
	err := h.store.RevertAgentGrantRedemption(context.WithoutCancel(ctx), grantID)
	if err != nil && !errors.Is(err, signup.ErrNotFound) {
		h.log.Error().Err(err).Str("grant_id", grantID).
			Msg("auth: revert agent grant redemption failed — grant stranded in redeemed state")
	}
}

// grantDisplayStatus reports the status a user should see, promoting an
// unexpired-but-time-passed pending grant to "expired" so the approval screen
// does not invite a decision that will be refused.
func grantDisplayStatus(g *signup.AgentGrant) string {
	if g.Status == "pending" && !g.ExpiresAt.After(time.Now()) {
		return "expired"
	}
	return g.Status
}
