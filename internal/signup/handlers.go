package signup

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"llm-pricing-api/internal/api"
)

// Handlers holds the signup flow dependencies and implements the HTTP handlers.
type Handlers struct {
	store  Store
	mailer Mailer
	issuer KeyIssuer
	guard  *AbuseGuard
	cfg    *HandlerConfig
	log    zerolog.Logger
}

// NewHandlers creates a Handlers instance with all dependencies wired.
func NewHandlers(store Store, mailer Mailer, issuer KeyIssuer, guard *AbuseGuard, cfg *HandlerConfig, log zerolog.Logger) *Handlers {
	return &Handlers{
		store:  store,
		mailer: mailer,
		issuer: issuer,
		guard:  guard,
		cfg:    cfg,
		log:    log,
	}
}

// Register mounts all signup routes onto the given Fiber router.
//
//	POST /auth/signup/request-link
//	GET  /auth/signup/verify
//	GET  /auth/signup/me          (requires valid session cookie)
//	POST /auth/signup/issue-key   (requires valid session cookie)
//	POST /auth/signup/regenerate-key (requires valid session cookie)
func (h *Handlers) Register(router fiber.Router) {
	router.Post("/request-link", h.RequestLink)
	router.Get("/verify", h.Verify)
	router.Get("/me", h.sessionMiddleware(), h.Me)
	router.Post("/issue-key", h.sessionMiddleware(), h.IssueKey)
	router.Post("/regenerate-key", h.sessionMiddleware(), h.RegenerateKey)
}

// ─── POST /auth/signup/request-link ──────────────────────────────────────────

type requestLinkBody struct {
	Email string `json:"email"`
}

// RequestLink accepts an email, applies abuse controls, and sends a magic-link.
// Always returns 200 (no account enumeration).
func (h *Handlers) RequestLink(c *fiber.Ctx) error {
	var body requestLinkBody
	if err := c.BodyParser(&body); err != nil || strings.TrimSpace(body.Email) == "" {
		return api.NewBadRequest("email is required")
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if !isValidEmail(email) {
		return api.NewBadRequest("invalid email address")
	}

	ip := c.IP()
	if err := h.guard.CheckRequestLink(c.Context(), ip, email); err != nil {
		// Log the specific reason internally but return a generic response to
		// avoid leaking which control was triggered.
		h.log.Warn().Str("ip", ip).Str("email", email).Err(err).Msg("signup: request-link blocked")
		// Deliberate: still return 200 to prevent enumeration via HTTP status.
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"message": "If that email is valid, you'll receive a link shortly.",
		})
	}

	identity, err := h.store.UpsertIdentity(c.Context(), email, hashValue(ip), hashValue(c.Get("User-Agent")))
	if err != nil {
		h.log.Error().Err(err).Str("email", email).Msg("signup: upsert identity failed")
		return api.NewInternalError("could not process request")
	}

	rawToken, tokenHash, magicLinkURL, expiresAt, err := GenerateToken(TokenConfig{
		SigningSecret: h.cfg.SigningSecret,
		TTL:           h.cfg.TokenTTL,
		BaseURL:       h.cfg.MagicLinkBase,
		Path:          h.cfg.MagicLinkPath,
	})
	_ = rawToken // only stored as hash
	if err != nil {
		h.log.Error().Err(err).Msg("signup: token generation failed")
		return api.NewInternalError("could not generate link")
	}

	if _, err := h.store.InsertToken(c.Context(), identity.ID, tokenHash, expiresAt); err != nil {
		h.log.Error().Err(err).Msg("signup: insert token failed")
		return api.NewInternalError("could not store token")
	}

	ttlMinutes := int(h.cfg.TokenTTL.Minutes())
	if err := h.mailer.SendMagicLink(c.Context(), email, magicLinkURL, ttlMinutes); err != nil {
		h.log.Error().Err(err).Str("email", email).Msg("signup: email delivery failed")
		// Don't expose delivery failure — return success anyway.
	}

	h.log.Info().Str("identity_id", identity.ID).Msg("signup: magic-link issued")
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "If that email is valid, you'll receive a link shortly.",
	})
}

// ─── GET /auth/signup/verify ──────────────────────────────────────────────────

// Verify validates the magic-link token, marks the identity verified, and sets
// a signed session cookie. On success it redirects to the key reveal page.
func (h *Handlers) Verify(c *fiber.Ctx) error {
	signed := c.Query("token")
	if signed == "" {
		return api.NewBadRequest("token is required")
	}

	_, tokenHash, err := ParseToken(signed, h.cfg.SigningSecret)
	if err != nil {
		// Log a hash-derived prefix — never log raw or truncated credential material.
		h.log.Warn().Str("token_sha256_prefix", truncate(hashValue(signed), 12)).Err(err).Msg("signup: token parse failed")
		return api.NewUnauthorized("invalid or expired link")
	}

	token, err := h.store.ConsumeToken(c.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, ErrTokenConsumed) || errors.Is(err, ErrTokenExpired) || errors.Is(err, ErrNotFound) {
			return api.NewUnauthorized("invalid or expired link")
		}
		h.log.Error().Err(err).Msg("signup: consume token failed")
		return api.NewInternalError("could not verify link")
	}

	if err := h.store.MarkEmailVerified(c.Context(), token.IdentityID); err != nil {
		// Non-fatal — identity may already be verified.
		h.log.Warn().Err(err).Str("identity_id", token.IdentityID).Msg("signup: mark verified returned error")
	}

	session := Session{
		IdentityID: token.IdentityID,
		ExpiresAt:  time.Now().Add(h.cfg.SessionTTL),
	}
	cookieVal, err := EncodeSession(session, h.cfg.SigningSecret)
	if err != nil {
		h.log.Error().Err(err).Msg("signup: encode session failed")
		return api.NewInternalError("could not create session")
	}

	c.Cookie(&fiber.Cookie{
		Name:     h.cfg.SessionCookieName,
		Value:    cookieVal,
		Expires:  session.ExpiresAt,
		HTTPOnly: true,
		Secure:   h.cfg.SessionSecure,
		SameSite: h.cfg.SessionSameSite,
	})

	h.log.Info().Str("identity_id", token.IdentityID).Msg("signup: email verified, session created")

	// Redirect to the key reveal page (frontend handles issue-key call).
	siteURL := h.cfg.MagicLinkBase
	return c.Redirect(siteURL+"/signup/free?verified=1", fiber.StatusFound)
}

// ─── GET /auth/signup/me ──────────────────────────────────────────────────────

// Me returns the authenticated identity's email and key status.
func (h *Handlers) Me(c *fiber.Ctx) error {
	sess, ok := sessionFromCtx(c)
	if !ok {
		return api.NewUnauthorized("authentication required")
	}

	identity, err := h.store.GetIdentityByID(c.Context(), sess.IdentityID)
	if err != nil {
		return api.NewUnauthorized("identity not found")
	}

	activeKey, _ := h.store.GetActiveKey(c.Context(), identity.ID)

	resp := fiber.Map{
		"identity_id": identity.ID,
		"email":       identity.Email,
		"verified":    identity.EmailVerifiedAt != nil,
		"has_key":     activeKey != nil,
	}
	if activeKey != nil {
		resp["key_created_at"] = activeKey.CreatedAt
	}
	return c.JSON(fiber.Map{"data": resp})
}

// ─── POST /auth/signup/issue-key ──────────────────────────────────────────────

// IssueKey creates or returns the existing active key for the verified identity.
func (h *Handlers) IssueKey(c *fiber.Ctx) error {
	sess, ok := sessionFromCtx(c)
	if !ok {
		return api.NewUnauthorized("authentication required")
	}

	// Return existing key metadata if one already exists.
	existingKey, err := h.store.GetActiveKey(c.Context(), sess.IdentityID)
	if err == nil {
		// Key exists — return metadata only (no plaintext re-reveal).
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"status":      "existing",
				"created_at":  existingKey.CreatedAt,
				"message":     "You already have an active API key. Use regenerate-key to replace it.",
			},
		})
	}
	if !errors.Is(err, ErrNotFound) {
		h.log.Error().Err(err).Str("identity_id", sess.IdentityID).Msg("signup: get active key failed")
		return api.NewInternalError("could not check key status")
	}

	// Create a new key in Unkey.
	providerKeyID, plaintext, err := h.issuer.CreateKey(c.Context(), h.cfg.UnkeyAPIID, sess.IdentityID)
	if err != nil {
		h.log.Error().Err(err).Str("identity_id", sess.IdentityID).Msg("signup: create Unkey key failed")
		return api.NewInternalError("could not issue API key")
	}

	// Persist to registry.
	if _, err := h.store.InsertKey(c.Context(), sess.IdentityID, providerKeyID); err != nil {
		// Revoke the Unkey key we just created — DB is the source of truth.
		if rErr := h.issuer.RevokeKey(c.Context(), providerKeyID); rErr != nil {
			h.log.Error().Err(rErr).Str("provider_key_id", providerKeyID).Msg("signup: rollback revoke failed")
		}
		// ErrDuplicateActiveKey: a concurrent request already inserted a key
		// (race on the unique partial index). The new Unkey key has been revoked;
		// return the same "existing key" response as the early-exit path above.
		if errors.Is(err, ErrDuplicateActiveKey) {
			h.log.Warn().Str("identity_id", sess.IdentityID).Msg("signup: duplicate active key race — returning existing")
			existing, gErr := h.store.GetActiveKey(c.Context(), sess.IdentityID)
			if gErr != nil {
				return api.NewInternalError("could not retrieve existing key")
			}
			return c.JSON(fiber.Map{
				"data": fiber.Map{
					"status":     "existing",
					"created_at": existing.CreatedAt,
					"message":    "You already have an active API key. Use regenerate-key to replace it.",
				},
			})
		}
		h.log.Error().Err(err).Str("identity_id", sess.IdentityID).Msg("signup: insert key registry failed")
		return api.NewInternalError("could not persist key")
	}

	h.log.Info().Str("identity_id", sess.IdentityID).Msg("signup: API key issued")
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"data": fiber.Map{
			"status":  "issued",
			"key":     plaintext, // shown once only
			"message": "Save this key — it will not be shown again.",
		},
	})
}

// ─── POST /auth/signup/regenerate-key ─────────────────────────────────────────

// RegenerateKey revokes the existing key and issues a new one.
func (h *Handlers) RegenerateKey(c *fiber.Ctx) error {
	sess, ok := sessionFromCtx(c)
	if !ok {
		return api.NewUnauthorized("authentication required")
	}

	if err := h.guard.CheckRegenerateKey(c.Context(), sess.IdentityID); err != nil {
		return api.NewTooManyRequests(err.Error())
	}

	oldKey, err := h.store.GetActiveKey(c.Context(), sess.IdentityID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return api.NewInternalError("could not check key status")
	}

	// Issue new key in Unkey first — we can roll back if the DB write fails.
	providerKeyID, plaintext, err := h.issuer.CreateKey(c.Context(), h.cfg.UnkeyAPIID, sess.IdentityID)
	if err != nil {
		h.log.Error().Err(err).Str("identity_id", sess.IdentityID).Msg("signup: create Unkey key failed (regen)")
		return api.NewInternalError("could not create new key")
	}

	// Atomically revoke old + insert new in the DB so the one-active-key
	// invariant is never violated (no window where both are active or neither is).
	var oldProviderKeyID string
	if oldKey != nil {
		oldProviderKeyID = oldKey.ProviderKeyID
	}
	if _, err := h.store.RevokeAndInsertKey(c.Context(), sess.IdentityID, oldProviderKeyID, providerKeyID); err != nil {
		// DB failed — roll back the Unkey key we just created.
		if rErr := h.issuer.RevokeKey(c.Context(), providerKeyID); rErr != nil {
			h.log.Error().Err(rErr).Msg("signup: rollback new key revoke failed")
		}
		return api.NewInternalError("could not persist new key")
	}

	// DB committed — now revoke the old key in Unkey.
	// Retried up to 3 times with brief back-off. The DB already marks the key
	// as revoked, but Unkey auth validates against Unkey's own records, so a
	// persistent failure here means the old key stays live until reconciled.
	// If all retries fail, a structured ERROR is logged with provider_key_id so
	// an operator can manually revoke via the Unkey dashboard or a reconciliation job.
	if oldKey != nil {
		const maxRevocationAttempts = 3
		var revokeErr error
		for attempt := 1; attempt <= maxRevocationAttempts; attempt++ {
			revokeErr = h.issuer.RevokeKey(c.Context(), oldKey.ProviderKeyID)
			if revokeErr == nil {
				break
			}
			if attempt < maxRevocationAttempts {
				time.Sleep(time.Duration(attempt*200) * time.Millisecond)
			}
		}
		if revokeErr != nil {
			// Log at ERROR level — old key is still valid in Unkey despite DB revocation.
			// Requires manual reconciliation via Unkey dashboard (revoke key ID below).
			h.log.Error().
				Err(revokeErr).
				Str("provider_key_id", oldKey.ProviderKeyID).
				Str("identity_id", sess.IdentityID).
				Int("attempts", maxRevocationAttempts).
				Msg("signup: revoke old Unkey key failed after retries — MANUAL RECONCILIATION REQUIRED")
		}
	}

	h.log.Info().Str("identity_id", sess.IdentityID).Msg("signup: API key regenerated")
	return c.JSON(fiber.Map{
		"data": fiber.Map{
			"status":  "regenerated",
			"key":     plaintext,
			"message": "Save this key — it will not be shown again.",
		},
	})
}

// ─── Session middleware ───────────────────────────────────────────────────────

const ctxKeySession = "signup_session"

// sessionMiddleware reads and validates the session cookie.
func (h *Handlers) sessionMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		cookieVal := c.Cookies(h.cfg.SessionCookieName)
		if cookieVal == "" {
			return api.NewUnauthorized("authentication required")
		}
		sess, err := DecodeSession(cookieVal, h.cfg.SigningSecret)
		if err != nil {
			return api.NewUnauthorized("session expired or invalid")
		}
		c.Locals(ctxKeySession, sess)
		return c.Next()
	}
}

func sessionFromCtx(c *fiber.Ctx) (*Session, bool) {
	sess, ok := c.Locals(ctxKeySession).(*Session)
	return sess, ok
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func isValidEmail(email string) bool {
	at := strings.Index(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}
	dot := strings.LastIndex(email[at:], ".")
	return dot > 1 && at+dot < len(email)-1
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
