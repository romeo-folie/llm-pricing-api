// Package auth implements the magic-link signup HTTP endpoints:
//
//	POST /auth/signup/request-link  — request a one-time verification email
//	GET  /auth/signup/verify        — consume token, set session cookie
//	GET  /auth/signup/me            — return the verified identity (session required)
package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

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
	InsertKey(ctx context.Context, identityID, providerKeyID string) (*signup.KeyRecord, error)
	DeleteExpiredTokens(ctx context.Context) (int64, error)
}

// Mailer is the subset of mailer.Mailer that auth handlers require.
// Using an interface here keeps the handler testable without a live Resend key.
type Mailer interface {
	SendMagicLink(ctx context.Context, toEmail, verifyURL string) error
}

// Config carries the values from config.Config that auth handlers need.
type Config struct {
	MagicLinkTTLMinutes     int
	MagicLinkBaseURL        string
	MagicLinkPath           string
	SignupSessionCookieName string
	SignupSessionTTLHours   int
	SignupSessionSecure      bool
	// SigningSecret is used both to sign session cookies (HMAC) and as the
	// MAGIC_LINK_SIGNING_SECRET for any future token HMAC layer. Currently
	// token hashing uses plain SHA-256 (see signup.HashToken in store.go).
	SigningSecret string
	// SignupEnabled controls whether RequestLink accepts new signup requests.
	// When false, the endpoint returns 503 Service Unavailable.
	SignupEnabled bool
}

// Handler handles magic-link auth endpoints.
type Handler struct {
	store  Store
	mailer Mailer
	cfg    Config
	log    zerolog.Logger
}

// New constructs an auth Handler.
func New(store Store, mailer Mailer, cfg Config, log zerolog.Logger) *Handler {
	return &Handler{store: store, mailer: mailer, cfg: cfg, log: log}
}

// Register mounts the auth routes onto a Fiber router.
// The caller is expected to pass a router group already mounted at /auth.
func Register(router fiber.Router, h *Handler) {
	router.Post("/signup/request-link", h.RequestLink)
	router.Get("/signup/verify", h.Verify)
	router.Get("/signup/me", h.RequireSession, h.Me)
}

// ── POST /auth/signup/request-link ───────────────────────────────────────────

type requestLinkBody struct {
	Email string `json:"email"`
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

	log := logger.FromContext(c.Context(), h.log)

	var body requestLinkBody
	if err := c.BodyParser(&body); err != nil {
		return api.NewBadRequest("invalid request body")
	}
	email := normalizeEmail(body.Email)
	if !isValidEmail(email) {
		return api.NewBadRequest("invalid email address")
	}

	ctx := c.Context()

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

	verifyURL := signup.BuildVerifyURL(h.cfg.MagicLinkBaseURL, h.cfg.MagicLinkPath, rawToken)

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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
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

	ctx := c.Context()
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
	return c.Redirect(h.cfg.MagicLinkBaseURL+"/signup/verified", fiber.StatusFound)
}

// ── GET /auth/signup/me ───────────────────────────────────────────────────────

// Me returns the verified identity for the session cookie holder.
// RequireSession must run before this handler to populate locals.
func (h *Handler) Me(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	ident, err := h.store.GetIdentityByID(c.Context(), session.IdentityID)
	if err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return api.NewUnauthorized("identity not found")
		}
		return api.NewInternalError("internal error")
	}

	return c.JSON(fiber.Map{
		"identity_id":       ident.ID,
		"email":             ident.Email,
		"email_verified_at": ident.EmailVerifiedAt,
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
