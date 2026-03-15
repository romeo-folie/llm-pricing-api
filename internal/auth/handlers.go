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

	"llm-pricing-api/internal/signup"
)

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
	store  *signup.Store
	mailer Mailer
	cfg    Config
}

// New constructs an auth Handler.
func New(store *signup.Store, mailer Mailer, cfg Config) *Handler {
	return &Handler{store: store, mailer: mailer, cfg: cfg}
}

// Register mounts the auth routes onto a Fiber router.
func Register(router fiber.Router, h *Handler) {
	router.Post("/auth/signup/request-link", h.RequestLink)
	router.Get("/auth/signup/verify", h.Verify)
	router.Get("/auth/signup/me", h.Me)
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
	var body requestLinkBody
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	email := normalizeEmail(body.Email)
	if !isValidEmail(email) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid email address"})
	}

	ctx := c.Context()

	ipHash := hashField(realIP(c))
	uaHash := hashField(c.Get("User-Agent"))

	ident, err := h.store.CreateIdentity(ctx, email, ipHash, uaHash)
	if err != nil {
		// Log internally; return generic success to avoid enumeration.
		_ = err
		return genericOK(c)
	}

	rawToken, err := signup.GenerateRawToken()
	if err != nil {
		return genericOK(c)
	}

	expiresAt := time.Now().Add(time.Duration(h.cfg.MagicLinkTTLMinutes) * time.Minute)
	if _, err := h.store.CreateToken(ctx, ident.ID, rawToken, expiresAt); err != nil {
		return genericOK(c)
	}

	verifyURL := signup.BuildVerifyURL(h.cfg.MagicLinkBaseURL, h.cfg.MagicLinkPath, rawToken)

	// Send email asynchronously — delivery failure must not block the response
	// or reveal whether the email exists on the platform.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = h.mailer.SendMagicLink(bgCtx, email, verifyURL)
	}()

	return genericOK(c)
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
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing token"})
	}

	ctx := c.Context()

	// ConsumeToken is atomic: it marks used_at in a single UPDATE that also
	// enforces expires_at. Returns the identity_id on success.
	identityID, err := h.store.ConsumeToken(ctx, rawToken)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrNotFound):
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid token"})
		case errors.Is(err, signup.ErrTokenUsed):
			return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": "token already used"})
		case errors.Is(err, signup.ErrTokenExpired):
			return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": "token expired"})
		default:
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
		}
	}

	// Mark identity verified (idempotent for re-verify flows).
	if err := h.store.MarkEmailVerified(ctx, identityID); err != nil && !errors.Is(err, signup.ErrNotFound) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}

	ident, err := h.store.FindIdentityByID(ctx, identityID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}
	signup.SetSessionCookie(c, h.cfg.SignupSessionCookieName, sessionValue, h.cfg.SignupSessionTTLHours, h.cfg.SignupSessionSecure)

	return c.JSON(fiber.Map{
		"verified":    true,
		"identity_id": ident.ID,
		"email":       ident.Email,
	})
}

// ── GET /auth/signup/me ───────────────────────────────────────────────────────

// Me returns the verified identity for the session cookie holder.
func (h *Handler) Me(c *fiber.Ctx) error {
	session, err := h.sessionFromCookie(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not authenticated"})
	}

	ident, err := h.store.FindIdentityByID(c.Context(), session.IdentityID)
	if err != nil {
		if errors.Is(err, signup.ErrNotFound) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "identity not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
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
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not authenticated"})
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

// ── Helpers ───────────────────────────────────────────────────────────────────

func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isValidEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

func realIP(c *fiber.Ctx) string {
	if ip := c.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.Index(ip, ","); idx >= 0 {
			return strings.TrimSpace(ip[:idx])
		}
		return ip
	}
	return c.IP()
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
