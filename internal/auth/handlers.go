// Package auth implements the magic-link signup HTTP endpoints:
//
//	POST /auth/signup/request-link  — request a one-time verification email
//	GET  /auth/signup/verify        — consume token, set session cookie
//	GET  /auth/signup/me            — return the verified identity (session required)
//	POST /auth/signup/issue-key     — issue a new API key (session required)
//	POST /auth/signup/regenerate-key — revoke and reissue key (session required)
package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
	"llm-pricing-api/internal/logger"
	"llm-pricing-api/internal/middleware"
	"llm-pricing-api/internal/signup"
)

// Store is the subset of signup.Store that auth handlers require.
// Accepting an interface decouples the HTTP layer from the data-access
// concrete type and makes unit testing straightforward.
type Store interface {
	UpsertIdentity(ctx context.Context, email, ipHash, uaHash string) (*signup.Identity, error)
	GetIdentityByEmail(ctx context.Context, email string) (*signup.Identity, error)
	GetIdentityByID(ctx context.Context, id string) (*signup.Identity, error)
	InsertToken(ctx context.Context, identityID, tokenHash string, expiresAt time.Time) (*signup.MagicLinkToken, error)
	ConsumeToken(ctx context.Context, tokenHash string) (*signup.MagicLinkToken, error)
	MarkEmailVerified(ctx context.Context, identityID string) error
	GetActiveKey(ctx context.Context, identityID string) (*signup.KeyRecord, error)
	ListActiveKeys(ctx context.Context, identityID string) ([]signup.KeyRecord, error)
	InsertKey(ctx context.Context, identityID, providerKeyID string) (*signup.KeyRecord, error)
	InsertKeyWithLabel(ctx context.Context, identityID, providerKeyID, label, createdVia string) (*signup.KeyRecord, error)
	RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*signup.KeyRecord, error)
	// RevokeKeyByID revokes one key by its registry ID and returns the Unkey
	// provider key ID, which the handler needs in order to revoke it upstream.
	// alreadyRevoked reports a repeat call, so the upstream revocation can be
	// retried after a failure.
	RevokeKeyByID(ctx context.Context, identityID, keyID string) (providerKeyID string, alreadyRevoked bool, err error)
	DeleteExpiredTokens(ctx context.Context) (int64, error)

	// Agent device-grant operations (see agent.go).
	CreateAgentGrant(ctx context.Context, clientName, platform string, expiresAt time.Time) (*signup.AgentGrant, string, error)
	GetAgentGrantByDeviceCode(ctx context.Context, rawDeviceCode string) (*signup.AgentGrant, error)
	GetAgentGrantByUserCode(ctx context.Context, userCode string) (*signup.AgentGrant, error)
	DecideAgentGrant(ctx context.Context, userCode, identityID string, approve bool) (*signup.AgentGrant, error)
	RedeemAgentGrant(ctx context.Context, rawDeviceCode string) (*signup.AgentGrant, error)
	// RevertAgentGrantRedemption is the compensating action used when key
	// issuance fails after a grant has been claimed.
	RevertAgentGrantRedemption(ctx context.Context, grantID string) error
}

// Mailer is the subset of mailer.Mailer that auth handlers require.
// Using an interface here keeps the handler testable without a live Resend key.
type Mailer interface {
	SendMagicLink(ctx context.Context, toEmail, verifyURL string) error
}

// KeyIssuer abstracts Unkey key creation and revocation.
// Matches the signup.KeyIssuer interface — accepting it here allows sharing
// the same concrete NewUnkeyIssuer constructor from the signup package.
type KeyIssuer interface {
	CreateKey(ctx context.Context, apiID, ownedByID string) (providerKeyID, plaintext string, err error)
	RevokeKey(ctx context.Context, providerKeyID string) error
}

// Config carries the values from config.Config that auth handlers need.
type Config struct {
	MagicLinkTTLMinutes     int
	MagicLinkBaseURL        string
	MagicLinkPath           string
	SignupSessionCookieName string
	SignupSessionTTLHours   int
	SignupSessionSecure     bool
	// SigningSecret is used both to sign session cookies (HMAC) and as the
	// MAGIC_LINK_SIGNING_SECRET for any future token HMAC layer. Currently
	// token hashing uses plain SHA-256 (see signup.HashToken in store.go).
	SigningSecret string
	// SignupEnabled controls whether RequestLink accepts new signup requests.
	// When false, the endpoint returns 503 Service Unavailable. It also gates
	// the agent device-grant flow: no keys are minted while signup is off.
	SignupEnabled bool
	// AgentGrantTTLMinutes is how long a device grant stays redeemable.
	// Zero falls back to signup.AgentGrantTTL.
	AgentGrantTTLMinutes int
}

// maxAgentGrantTTLMinutes bounds a configured grant lifetime. A misconfigured
// value is clamped rather than honoured: an absurd TTL is either a typo or an
// attempt to mint a near-permanent authorization, and letting the multiplication
// overflow would produce a negative duration and an instantly-expired grant.
const maxAgentGrantTTLMinutes = 60

// agentGrantTTL returns the configured grant lifetime, defaulting and clamping
// when unset or out of range.
func (c Config) agentGrantTTL() time.Duration {
	if c.AgentGrantTTLMinutes <= 0 {
		return signup.AgentGrantTTL
	}
	if c.AgentGrantTTLMinutes > maxAgentGrantTTLMinutes {
		return maxAgentGrantTTLMinutes * time.Minute
	}
	return time.Duration(c.AgentGrantTTLMinutes) * time.Minute
}

// AbuseGuard enforces the signup abuse controls. Implemented by
// *signup.AbuseGuard; kept as an interface so tests can substitute or omit it.
type AbuseGuard interface {
	// CheckRequestLink applies the disposable-domain block, the per-IP hourly
	// rate limit, and the per-email resend cooldown.
	CheckRequestLink(ctx context.Context, ip, email string) error
	// CheckRegenerateKey applies the per-identity regeneration cooldown.
	CheckRegenerateKey(ctx context.Context, identityID string) error
	// CheckAgentDevice applies the per-IP device-grant rate limit.
	CheckAgentDevice(ctx context.Context, ip string) error
	// CheckUserCodeAttempt bounds lookups against a single user code.
	CheckUserCodeAttempt(ctx context.Context, userCode string) error
	// CheckCodeProbe bounds code lookups and decisions per IP. This is the
	// control that actually bounds code-space enumeration.
	CheckCodeProbe(ctx context.Context, ip string) error
	// CheckAgentPoll throttles token-endpoint polling per device code,
	// returning signup.ErrPollTooFast when the caller polls too soon.
	CheckAgentPoll(ctx context.Context, rawDeviceCode string) error
}

// Handler handles magic-link auth endpoints.
type Handler struct {
	store  Store
	mailer Mailer
	issuer KeyIssuer
	guard  AbuseGuard
	cfg    Config
	log    zerolog.Logger
}

// New constructs an auth Handler.
//
// guard may be nil, which disables every abuse control — acceptable in tests,
// but production must pass one: /auth/signup/request-link emails an arbitrary
// address, so without the per-email cooldown it can be used to mail-bomb a
// third party.
func New(store Store, mailer Mailer, issuer KeyIssuer, guard AbuseGuard, cfg Config, log zerolog.Logger) *Handler {
	return &Handler{store: store, mailer: mailer, issuer: issuer, guard: guard, cfg: cfg, log: log}
}

// Register mounts the magic-link signup and key-management routes.
// The caller is expected to pass a router group already mounted at /auth.
//
// The agent device-grant routes are registered separately by RegisterAgent so
// the caller can give them a poll-scale rate limit rather than the strict
// signup-shaped one.
func Register(router fiber.Router, h *Handler) {
	router.Post("/signup/request-link", h.RequestLink)
	router.Get("/signup/verify", h.Verify)
	router.Get("/signup/me", h.RequireSession, h.Me)
	router.Post("/signup/issue-key", h.RequireSession, h.IssueKey)
	router.Post("/signup/regenerate-key", h.RequireSession, h.RegenerateKey)
	// Key management. With multiple keys per identity a user needs to see what
	// exists and revoke one agent without disturbing the others.
	router.Get("/signup/keys", h.RequireSession, h.ListKeys)
	router.Delete("/signup/keys/:id", h.RequireSession, h.RevokeKey)
}

// RegisterAgent mounts the agent device-grant routes. The caller is expected to
// pass a router group already mounted at /auth/agent.
//
// These are registered separately from Register because an agent legitimately
// polls for approval every few seconds for the whole grant lifetime. Sharing the
// signup group's per-IP limit (10 requests per 15 minutes) would exhaust the
// bucket in under a minute and turn every subsequent poll into a 429, making the
// flow impossible to complete. The real poll control is the per-device-code
// throttle in AbuseGuard.CheckAgentPoll.
//
// /device and /token are unauthenticated by design: the agent has no key yet,
// which is the entire point. /grant and /approve require a session, so an
// unauthenticated caller learns nothing about whether a code exists.
func RegisterAgent(router fiber.Router, h *Handler) {
	router.Post("/device", h.DeviceGrant)
	router.Get("/grant", h.RequireSession, h.GetGrant)
	router.Post("/approve", h.RequireSession, h.ApproveGrant)
	router.Post("/token", h.RedeemGrant)
}

// ── POST /auth/signup/request-link ───────────────────────────────────────────

type requestLinkBody struct {
	Email string `json:"email"`
	// Next is an optional same-site path to return to after verification. The
	// agent approval flow uses it to send the user back to /activate?code=XXXX
	// so an approval survives the email round trip. Validated by
	// signup.SafeNextPath — never trusted as given.
	Next string `json:"next"`
}

// RequestLink accepts an email, upserts an identity row, mints a one-time
// token, and fires a magic-link email.
//
// Returns 200 with the same generic message in normal operation to prevent
// account enumeration. Returns 503 when signup is disabled (SIGNUP_ENABLED=false).
func (h *Handler) RequestLink(c *fiber.Ctx) error {
	if !h.cfg.SignupEnabled {
		return api.NewServiceUnavailable("signup is currently disabled")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	var body requestLinkBody
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid request body")
	}
	email := normalizeEmail(body.Email)
	if !isValidEmail(email) {
		return api.NewBadRequest("invalid email address")
	}

	ctx := c.UserContext()

	if h.guard != nil {
		switch err := h.guard.CheckRequestLink(ctx, middleware.RealIP(c), email); {
		case err == nil:
			// allowed
		case errors.Is(err, signup.ErrDisposableDomain):
			// About the caller's own input, so it is safe to say so plainly.
			return api.NewBadRequest(err.Error())
		case errors.Is(err, signup.ErrRateLimited):
			// About the caller, not the address — safe to surface.
			return api.NewTooManyRequests(err.Error())
		case errors.Is(err, signup.ErrResendCooldown):
			// Deliberately indistinguishable from success: reporting the cooldown
			// would reveal that a link was recently sent to this address, and the
			// point of the cooldown is to protect that inbox. Skip the send.
			log.Info().Str("email_hash", truncate(hashField(email), 12)).Msg("auth: resend cooldown active — suppressing send")
			return genericOK(c)
		default:
			log.Error().Err(err).Msg("auth: abuse guard error — allowing request")
		}
	}

	ipHash := hashField(middleware.RealIP(c))
	uaHash := hashField(c.Get("User-Agent"))

	ident, err := h.store.UpsertIdentity(ctx, email, ipHash, uaHash)
	if err != nil {
		log.Error().Err(err).Str("email_hash", truncate(hashField(email), 12)).Msg("auth: upsert identity failed")
		return genericOK(c)
	}

	rawToken, err := signup.GenerateRawToken()
	if err != nil {
		log.Error().Err(err).Msg("auth: generate token failed")
		return genericOK(c)
	}

	tokenHash := signup.HashToken(rawToken)
	expiresAt := time.Now().Add(time.Duration(h.cfg.MagicLinkTTLMinutes) * time.Minute)
	if _, err := h.store.InsertToken(ctx, ident.ID, tokenHash, expiresAt); err != nil {
		log.Error().Err(err).Str("identity_id", ident.ID).Msg("auth: insert token failed")
		return genericOK(c)
	}

	verifyURL := signup.BuildVerifyURLWithNext(h.cfg.MagicLinkBaseURL, h.cfg.MagicLinkPath, rawToken, body.Next)

	// Send email synchronously with a short timeout. Rate limiting (fix #1)
	// bounds how many requests reach this point, so blocking is safe.
	// Delivery failure must not leak whether the email exists.
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(c.UserContext()), 10*time.Second)
	defer cancel()
	if sendErr := h.mailer.SendMagicLink(sendCtx, email, verifyURL); sendErr != nil {
		log.Warn().Err(sendErr).Str("email_hash", truncate(hashField(email), 12)).Msg("auth: magic-link email delivery failed")
	}

	return genericOK(c)
}

// truncate shortens s to at most n runes, for log fields.
//
// It counts runes rather than bytes on purpose: agent-supplied names may contain
// multi-byte characters (validation permits them), and a byte slice through a
// rune emits invalid UTF-8 into the log pipeline and any downstream consumer.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func genericOK(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"message": "If that email is valid, you'll receive a sign-in link shortly.",
	})
}

// ── GET /auth/signup/verify?token=... ────────────────────────────────────────

// Verify consumes a magic-link token atomically, marks the identity verified,
// and issues a signed session cookie.
func (h *Handler) Verify(c *fiber.Ctx) error {
	rawToken := strings.TrimSpace(c.Query("token"))
	if rawToken == "" {
		return api.NewBadRequest("missing token")
	}

	ctx := c.UserContext()
	log := logger.FromContext(ctx, h.log)

	// Hash the raw token before looking it up — the DB stores the hash, not
	// the raw value, to prevent offline brute-force from a DB leak.
	tokenHash := signup.HashToken(rawToken)

	// ConsumeToken is atomic: it marks used_at in a single UPDATE that also
	// enforces expires_at. Returns the consumed token row (including identity_id).
	tok, err := h.store.ConsumeToken(ctx, tokenHash)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrNotFound):
			// Redirect to the frontend error page rather than returning raw JSON.
			// The frontend /signup/free page renders a user-friendly message for
			// each error code.
			return c.Redirect(h.cfg.MagicLinkBaseURL+"/signup/free?error=invalid-link", fiber.StatusFound)
		case errors.Is(err, signup.ErrTokenConsumed):
			return c.Redirect(h.cfg.MagicLinkBaseURL+"/signup/free?error=link-used", fiber.StatusFound)
		case errors.Is(err, signup.ErrTokenExpired):
			return c.Redirect(h.cfg.MagicLinkBaseURL+"/signup/free?error=link-expired", fiber.StatusFound)
		default:
			log.Error().Err(err).Msg("auth: consume token failed")
			return c.Redirect(h.cfg.MagicLinkBaseURL+"/signup/free?error=server-error", fiber.StatusFound)
		}
	}

	// Mark identity verified (idempotent for re-verify flows).
	if err := h.store.MarkEmailVerified(ctx, tok.IdentityID); err != nil && !errors.Is(err, signup.ErrNotFound) {
		log.Error().Err(err).Str("identity_id", tok.IdentityID).Msg("auth: mark email verified failed")
		return api.NewInternalError("internal error")
	}

	ident, err := h.store.GetIdentityByID(ctx, tok.IdentityID)
	if err != nil {
		log.Error().Err(err).Str("identity_id", tok.IdentityID).Msg("auth: get identity failed")
		return api.NewInternalError("internal error")
	}

	// Issue signed session cookie.
	now := time.Now()
	payload := signup.SessionPayload{
		IdentityID: ident.ID,
		Email:      ident.Email,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(time.Duration(h.cfg.SignupSessionTTLHours) * time.Hour).Unix(),
	}
	sessionValue, err := signup.SignSession(h.cfg.SigningSecret, payload)
	if err != nil {
		log.Error().Err(err).Msg("auth: sign session failed")
		return api.NewInternalError("internal error")
	}
	setSessionCookie(c, h.cfg.SignupSessionCookieName, sessionValue, h.cfg.SignupSessionTTLHours, h.cfg.SignupSessionSecure)

	// Redirect to the frontend key-reveal page. The frontend reads the session
	// cookie (via GET /auth/signup/me) and shows the API key.
	//
	// A validated same-site `next` takes precedence: the agent approval flow
	// sends the user back to /activate?code=XXXX so the approval they started
	// survives the trip through their inbox. SafeNextPath rejects anything that
	// is not a same-origin absolute path, which is what keeps this from being an
	// open redirect.
	base := strings.TrimRight(h.cfg.MagicLinkBaseURL, "/")
	dest := base + "/signup/verified"
	if next := signup.SafeNextPath(c.Query("next")); next != "" {
		dest = base + next
	}
	return c.Redirect(dest, fiber.StatusFound)
}

// ── GET /auth/signup/me ───────────────────────────────────────────────────────

// Me returns the verified identity for the session cookie holder.
// RequireSession must run before this handler to populate locals.
//
// Response includes has_active_key and email_verified so the frontend
// /signup/verified page can decide whether to call issue-key.
func (h *Handler) Me(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	ident, err := h.store.GetIdentityByID(c.UserContext(), session.IdentityID)
	if err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return api.NewUnauthorized("identity not found")
		}
		return api.NewInternalError("internal error")
	}

	// Report the identity's active keys. A DB failure here is logged and
	// reported as zero rather than failing /me outright: the page can still
	// render, and the error is visible in logs.
	keys, keyErr := h.store.ListActiveKeys(c.UserContext(), ident.ID)
	if keyErr != nil {
		log := logger.FromContext(c.UserContext(), h.log)
		log.Warn().Err(keyErr).Str("identity_id", ident.ID).Msg("auth: ListActiveKeys error in /me — reporting zero keys")
		keys = nil
	}

	return c.JSON(fiber.Map{
		"id":             ident.ID,
		"email":          ident.Email,
		"email_verified": ident.EmailVerifiedAt != nil,
		"has_active_key": len(keys) > 0,
		"key_count":      len(keys),
		"max_keys":       signup.MaxActiveKeysPerIdentity,
	})
}

// ── POST /auth/signup/issue-key ───────────────────────────────────────────────

// IssueKey creates a new Unkey API key for the session holder.
// If the identity already has an active key, returns metadata only (no plaintext re-reveal).
// RequireSession must run before this handler.
func (h *Handler) IssueKey(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	// Verify identity still exists.
	if _, err := h.store.GetIdentityByID(c.UserContext(), session.IdentityID); err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return api.NewUnauthorized("identity not found")
		}
		return api.NewInternalError("could not verify identity")
	}

	// Reveal-once: if the identity already holds any active key, return metadata
	// only. A plaintext is shown exactly once at creation and cannot be
	// re-derived, so the recovery path is regenerate-key targeting a specific
	// key (see GET /auth/signup/keys).
	existing, err := h.store.ListActiveKeys(c.UserContext(), session.IdentityID)
	if err != nil {
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: list active keys failed")
		return api.NewInternalError("could not check key status")
	}
	if len(existing) > 0 {
		return c.JSON(fiber.Map{
			"status":     "existing",
			"key_count":  len(existing),
			"max_keys":   signup.MaxActiveKeysPerIdentity,
			"created_at": existing[0].CreatedAt,
			"message":    "You already have an active API key. Use regenerate-key to replace it, or GET /auth/signup/keys to review your keys.",
		})
	}

	// Create a new Unkey key.
	providerKeyID, plaintext, err := h.issuer.CreateKey(c.UserContext(), "", session.IdentityID) // apiID omitted — issuer uses its stored value
	if err != nil {
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: create Unkey key failed")
		return api.NewInternalError("could not issue API key")
	}

	// Persist the key record.
	if _, err := h.store.InsertKeyWithLabel(c.UserContext(), session.IdentityID, providerKeyID, "", signup.CreatedViaMagicLink); err != nil {
		// Clean up the dangling Unkey key — use a background context so
		// request cancellation doesn't silently skip the revocation.
		_ = h.issuer.RevokeKey(context.WithoutCancel(c.UserContext()), providerKeyID)
		if errors.Is(err, signup.ErrActiveKeyLimitReached) {
			// Raced with a concurrent issue (e.g. an agent grant approving at
			// the same moment); the Unkey key above has already been revoked.
			return api.NewConflict("active key limit reached — revoke a key before issuing another")
		}
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: insert key record failed")
		return api.NewInternalError("could not persist API key")
	}

	return c.JSON(fiber.Map{
		"plaintext":       plaintext,
		"provider_key_id": providerKeyID,
	})
}

// ── POST /auth/signup/regenerate-key ─────────────────────────────────────────

// RegenerateKey rotates one key and issues a fresh one, returning the new
// plaintext (the only way to obtain a plaintext for an existing identity).
//
// The target is chosen from the optional body {"key_id": "..."}. Without it the
// newest MAGIC-LINK key is rotated, because that is the key the browser flow
// owns and silently rotating an agent's key would break that agent. If there is
// no magic-link key and a slot is free a fresh key is minted instead; if every
// slot is held by an agent key the request is refused with 409 so the caller
// makes an explicit choice.
//
// Uses RevokeAndInsertKey for the atomic DB update (old → revoked, new → active).
// RequireSession must run before this handler.
func (h *Handler) RegenerateKey(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	// Optional body: {"key_id": "..."} selects which key to rotate. Older
	// callers send no body at all, which is valid as long as only one key is
	// active. A missing/invalid body is not an error.
	var body struct {
		KeyID string `json:"key_id"`
	}
	_ = c.BodyParser(&body)

	if h.guard != nil {
		if err := h.guard.CheckRegenerateKey(c.UserContext(), session.IdentityID); err != nil {
			if errors.Is(err, signup.ErrRegenerateCooldown) {
				return api.NewTooManyRequests(err.Error())
			}
			log.Error().Err(err).Msg("auth: regenerate guard error — allowing request")
		}
	}

	// Verify identity still exists.
	if _, err := h.store.GetIdentityByID(c.UserContext(), session.IdentityID); err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return api.NewUnauthorized("identity not found")
		}
		return api.NewInternalError("could not verify identity")
	}

	// Choose which key to replace. With multiple keys active, rotating an
	// unspecified one would silently revoke an unrelated agent's key, so an
	// explicit key_id is required once more than one exists.
	keys, err := h.store.ListActiveKeys(c.UserContext(), session.IdentityID)
	if err != nil {
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: list active keys failed (regenerate)")
		return api.NewInternalError("could not fetch existing key")
	}

	var target *signup.KeyRecord
	switch {
	case body.KeyID != "":
		for i := range keys {
			if keys[i].ID == body.KeyID {
				target = &keys[i]
				break
			}
		}
		if target == nil {
			return api.NewNotFound("no active key with that id")
		}
	default:
		// No key named. The browser flow calls this with no body and its intent
		// is "give me a usable key in this browser", so the default target is the
		// newest MAGIC-LINK key — the key that flow owns. Rotating an agent's key
		// on the user's behalf would break that agent silently.
		for i := range keys {
			if keys[i].CreatedVia == signup.CreatedViaMagicLink {
				target = &keys[i]
				break
			}
		}
		if target == nil && len(keys) >= signup.MaxActiveKeysPerIdentity {
			// Every slot is taken by agent keys. Minting would exceed the cap and
			// rotating one would break an agent, so ask for an explicit choice.
			return api.NewConflict(
				"no magic-link key to replace and the active key limit is reached — revoke a key first (see GET /auth/signup/keys)")
		}
		// Otherwise target stays nil and a fresh key is issued below, using
		// whatever slot is free.
	}

	var oldProviderKeyID string
	if target != nil {
		oldProviderKeyID = target.ProviderKeyID
	}

	// Safe order: CREATE new key first, THEN revoke old one.
	// Reversing this risks leaving the user with no key if CreateKey fails after
	// the old key is already revoked.
	providerKeyID, plaintext, err := h.issuer.CreateKey(c.UserContext(), "", session.IdentityID) // apiID omitted — issuer uses its stored value
	if err != nil {
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: create replacement key failed")
		return api.NewInternalError("could not issue replacement key")
	}

	// Atomically revoke old DB record and insert new one.
	if _, err := h.store.RevokeAndInsertKey(c.UserContext(), session.IdentityID, oldProviderKeyID, providerKeyID); err != nil {
		// Clean up the newly created Unkey key — use a background context so
		// cancellation of the request context doesn't silently skip the revocation.
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.UserContext()), 10*time.Second)
		defer cancel()
		if revokeErr := h.issuer.RevokeKey(revokeCtx, providerKeyID); revokeErr != nil {
			log.Error().Err(revokeErr).Str("provider_key_id", providerKeyID).
				Msg("auth: failed to revoke unused replacement key — it has no registry row and may still authenticate")
		}
		// A concurrent rotation can win the cap between the pre-check above and
		// this insert. That is a conflict, not an internal failure, and the same
		// sentinel maps to 409 in IssueKey.
		if errors.Is(err, signup.ErrActiveKeyLimitReached) {
			return api.NewConflict("active key limit reached — revoke a key before regenerating")
		}
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: RevokeAndInsertKey failed")
		return api.NewInternalError("could not persist replacement key")
	}

	// Revoke the replaced key upstream, after the DB is committed.
	//
	// This stays best-effort rather than failing the request: the user's actual
	// goal — a working replacement key — already succeeded, and the new plaintext
	// is only delivered once. So the failure is reported explicitly instead of
	// being swallowed, because /v1 trusts Unkey, not this registry: until the
	// call succeeds the old credential is still live.
	var revocationWarning string
	if oldProviderKeyID != "" {
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.UserContext()), 10*time.Second)
		defer cancel()
		if revokeErr := h.issuer.RevokeKey(revokeCtx, oldProviderKeyID); revokeErr != nil {
			log.Error().Err(revokeErr).Str("provider_key_id", oldProviderKeyID).
				Msg("auth: replaced key could not be revoked upstream — it may still authenticate")
			revocationWarning = "the replaced key could not be revoked upstream and may still authenticate; revoke it again from your key list"
		}
	}

	resp := fiber.Map{
		"plaintext":       plaintext,
		"provider_key_id": providerKeyID,
	}
	if revocationWarning != "" {
		resp["revocation_warning"] = revocationWarning
	}
	return c.JSON(resp)
}

// ── GET /auth/signup/keys ────────────────────────────────────────────────────

// ListKeys returns the session holder's active keys. Plaintext is never
// included: a key's secret exists only in the response that created it.
// RequireSession must run before this handler.
func (h *Handler) ListKeys(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	log := logger.FromContext(c.UserContext(), h.log)
	keys, err := h.store.ListActiveKeys(c.UserContext(), session.IdentityID)
	if err != nil {
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: list active keys failed")
		return api.NewInternalError("could not list keys")
	}

	out := make([]fiber.Map, 0, len(keys))
	for _, k := range keys {
		out = append(out, fiber.Map{
			"id": k.ID,
			// label is agent-supplied and untrusted; the frontend must render it
			// as text, never as markup.
			"label":       k.Label,
			"created_via": k.CreatedVia,
			"created_at":  k.CreatedAt,
			// provider_key_id is deliberately omitted — it is an internal Unkey
			// identifier with no client-side use.
		})
	}

	return c.JSON(fiber.Map{
		"keys":     out,
		"count":    len(out),
		"max_keys": signup.MaxActiveKeysPerIdentity,
	})
}

// ── DELETE /auth/signup/keys/:id ─────────────────────────────────────────────

// RevokeKey revokes one of the session holder's keys and reports success only
// once the key can no longer authenticate.
//
// Order matters. /v1 authorises against Unkey, not against this registry, so a
// registry-only revocation is not a revocation: the credential keeps working.
// The registry row is disabled first (idempotent, so a retry is safe), but if
// the upstream revocation then fails the request answers 502 rather than
// claiming success. Returning 200 here would tell a user their leaked key is
// dead when it is still live, and nothing would ever retry it.
//
// The store tolerates an already-revoked row precisely so this retry works: a
// second attempt re-drives the upstream call and succeeds once Unkey recovers.
//
// The lookup is scoped to the session identity, so another identity's key ID is
// indistinguishable from a missing one.
// RequireSession must run before this handler.
func (h *Handler) RevokeKey(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	keyID := strings.TrimSpace(c.Params("id"))
	if keyID == "" {
		return api.NewBadRequest("missing key id")
	}

	log := logger.FromContext(c.UserContext(), h.log)

	providerKeyID, alreadyRevoked, err := h.store.RevokeKeyByID(c.UserContext(), session.IdentityID, keyID)
	if err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return api.NewNotFound("no key with that id")
		}
		log.Error().Err(err).Str("identity_id", session.IdentityID).Msg("auth: revoke key failed")
		return api.NewInternalError("could not revoke key")
	}

	// Detached context: the local revocation has already committed, so this must
	// still be attempted if the client disconnects. Bounded so a stuck upstream
	// cannot hold the connection and its pool slot open indefinitely.
	revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(c.UserContext()), 10*time.Second)
	defer cancel()

	if revokeErr := h.issuer.RevokeKey(revokeCtx, providerKeyID); revokeErr != nil {
		log.Error().Err(revokeErr).
			Str("provider_key_id", providerKeyID).
			Bool("already_revoked_locally", alreadyRevoked).
			Msg("auth: upstream revocation failed — key may still authenticate; retry this request")
		return api.NewBadGateway(
			"the key is disabled here but could not be revoked upstream, so it may still authenticate; retry this request")
	}

	return c.JSON(fiber.Map{
		"status": "revoked",
		"id":     keyID,
		// Surfaced so a caller retrying after a failure can tell the local
		// revocation was already done.
		"already_revoked": alreadyRevoked,
	})
}

// ── Session middleware ────────────────────────────────────────────────────────

// RequireSession validates the signup session cookie and stores the parsed
// SessionPayload under the "signup_session" local key.
// Returns 401 if the cookie is absent, tampered, or expired.
func (h *Handler) RequireSession(c *fiber.Ctx) error {
	session, err := h.sessionFromCookie(c)
	if err != nil {
		return api.NewUnauthorized("not authenticated")
	}
	c.Locals("signup_session", session)
	return c.Next()
}

// SessionFromLocals retrieves the SessionPayload stored by RequireSession.
func SessionFromLocals(c *fiber.Ctx) (signup.SessionPayload, bool) {
	v := c.Locals("signup_session")
	if v == nil {
		return signup.SessionPayload{}, false
	}
	s, ok := v.(signup.SessionPayload)
	return s, ok
}

func (h *Handler) sessionFromCookie(c *fiber.Ctx) (signup.SessionPayload, error) {
	val := c.Cookies(h.cfg.SignupSessionCookieName)
	if val == "" {
		return signup.SessionPayload{}, errors.New("no session cookie")
	}
	return signup.VerifySession(h.cfg.SigningSecret, val)
}

// setSessionCookie writes the signed session cookie onto the Fiber response.
// This is HTTP-layer code and belongs in the auth package, not in signup.
func setSessionCookie(c *fiber.Ctx, name, value string, ttlHours int, secure bool) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   ttlHours * 3600,
		Secure:   secure,
		HTTPOnly: true,
		SameSite: "Lax",
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func normalizeEmail(raw string) string {
	trimmed := strings.TrimSpace(raw)
	// mail.ParseAddress accepts "Name <addr>" format; extract the bare address.
	if addr, err := mail.ParseAddress(trimmed); err == nil {
		return strings.ToLower(strings.TrimSpace(addr.Address))
	}
	return strings.ToLower(trimmed)
}

func isValidEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	// After normalizeEmail strips any display name, verify the result matches
	// the input — if not, the original had a display-name component.
	return strings.TrimSpace(addr.Address) == strings.TrimSpace(email)
}

// hashField returns a stable hex hash of s for use as an abuse signal.
// Using SHA-256 directly (no secret) is appropriate here: these hashes are
// opaque tokens for grouping, not secrets.
func hashField(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}
