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
	CreateIdentity(ctx context.Context, email, ipHash, uaHash string) (signup.Identity, error)
	CreateToken(ctx context.Context, identityID, rawToken string, expiresAt time.Time) (signup.Token, error)
	ConsumeToken(ctx context.Context, rawToken string) (string, error)
	MarkEmailVerified(ctx context.Context, identityID string) error
	FindIdentityByID(ctx context.Context, id string) (signup.Identity, error)
	CountRecentTokens(ctx context.Context, identityID string, since time.Time) (int, error)
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
// Always returns 200 with the same generic message to prevent account
// enumeration — no indication whether the address is new or existing.
func (h *Handler) RequestLink(c *fiber.Ctx) error {
	log := logger.FromContext(c.UserContext(), h.log)

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

	ident, err := h.store.CreateIdentity(ctx, email, ipHash, uaHash)
	if err != nil {
		log.Error().Err(err).Str("email_hash", truncate(hashField(email), 12)).Msg("auth: create identity failed")
		return genericOK(c)
	}

	// Rate-limit: suppress token creation if this identity already has too many
	// recent tokens. Returns 200 regardless to prevent account enumeration.
	const maxTokensPerWindow = 3
	window := time.Duration(h.cfg.MagicLinkTTLMinutes) * time.Minute
	recentCount, countErr := h.store.CountRecentTokens(ctx, ident.ID, time.Now().Add(-window))
	if countErr != nil {
		log.Error().Err(countErr).Msg("auth: count recent tokens failed")
		return genericOK(c)
	}
	if recentCount >= maxTokensPerWindow {
		log.Warn().Str("identity_id", ident.ID).Int("recent_tokens", recentCount).Msg("auth: rate limit exceeded, suppressing magic-link send")
		return genericOK(c)
	}

	rawToken, err := signup.GenerateRawToken()
	if err != nil {
		log.Error().Err(err).Msg("auth: generate token failed")
		return genericOK(c)
	}

	expiresAt := time.Now().Add(time.Duration(h.cfg.MagicLinkTTLMinutes) * time.Minute)
	if _, err := h.store.CreateToken(ctx, ident.ID, rawToken, expiresAt); err != nil {
		log.Error().Err(err).Str("identity_id", ident.ID).Msg("auth: create token failed")
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

	// ConsumeToken is atomic: it marks used_at in a single UPDATE that also
	// enforces expires_at. Returns the identity_id on success.
	identityID, err := h.store.ConsumeToken(ctx, rawToken)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrNotFound):
			return api.NewUnauthorized("invalid token")
		case errors.Is(err, signup.ErrTokenUsed):
			return api.NewGone("token already used")
		case errors.Is(err, signup.ErrTokenExpired):
			return api.NewGone("token expired")
		default:
			return api.NewInternalError("internal error")
		}
	}

	// Mark identity verified (idempotent for re-verify flows).
	if err := h.store.MarkEmailVerified(ctx, identityID); err != nil && !errors.Is(err, signup.ErrNotFound) {
		return api.NewInternalError("internal error")
	}

	ident, err := h.store.FindIdentityByID(ctx, identityID)
	if err != nil {
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
		return api.NewInternalError("internal error")
	}
	setSessionCookie(c, h.cfg.SignupSessionCookieName, sessionValue, h.cfg.SignupSessionTTLHours, h.cfg.SignupSessionSecure)

	return c.JSON(fiber.Map{
		"verified":    true,
		"identity_id": ident.ID,
		"email":       ident.Email,
	})
}

// ── GET /auth/signup/me ───────────────────────────────────────────────────────

// Me returns the verified identity for the session cookie holder.
// RequireSession must run before this handler to populate locals.
func (h *Handler) Me(c *fiber.Ctx) error {
	session, ok := SessionFromLocals(c)
	if !ok {
		return api.NewUnauthorized("not authenticated")
	}

	ident, err := h.store.FindIdentityByID(c.Context(), session.IdentityID)
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
