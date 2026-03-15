// Package signup provides the data-access layer for the free API-key
// onboarding flow: email identity management, magic-link token lifecycle,
// and the Unkey-backed API key registry.
//
// All functions use the package-level pgxpool injected by Init().
// Callers must call Init before using any store function.
package signup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a queried row does not exist.
var ErrNotFound = errors.New("signup: not found")

// ErrTokenUsed is returned when a magic-link token has already been consumed.
var ErrTokenUsed = errors.New("signup: token already used")

// ErrTokenExpired is returned when a magic-link token is past its expires_at.
var ErrTokenExpired = errors.New("signup: token expired")

var db *pgxpool.Pool

// Init sets the connection pool used by all store functions.
// Must be called once at process startup before any other signup function.
func Init(pool *pgxpool.Pool) { db = pool }

// ── Identity ─────────────────────────────────────────────────────────────────

// Identity represents a row in api_identities.
type Identity struct {
	ID              string
	Email           string
	EmailVerifiedAt *time.Time
	SignupIPHash    *string
	SignupUAHash    *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// normalizeEmail trims whitespace and lowercases an email address so all
// lookups are case-insensitive without a citext extension.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CreateIdentity inserts a new api_identities row and returns it.
// email is normalized before insert. Returns the existing row (ErrNotFound
// is never returned on duplicate — caller should use FindIdentityByEmail to
// detect existing registrations before calling).
func CreateIdentity(ctx context.Context, email, ipHash, uaHash string) (Identity, error) {
	email = normalizeEmail(email)
	var id Identity
	err := db.QueryRow(ctx, `
		INSERT INTO api_identities (email, signup_ip_hash, signup_ua_hash)
		VALUES ($1, $2, $3)
		RETURNING id, email, email_verified_at, signup_ip_hash, signup_ua_hash, created_at, updated_at`,
		email, nullStr(ipHash), nullStr(uaHash),
	).Scan(
		&id.ID, &id.Email, &id.EmailVerifiedAt,
		&id.SignupIPHash, &id.SignupUAHash,
		&id.CreatedAt, &id.UpdatedAt,
	)
	if err != nil {
		return Identity{}, fmt.Errorf("signup.CreateIdentity: %w", err)
	}
	return id, nil
}

// FindIdentityByEmail returns the identity for the given (normalized) email.
// Returns ErrNotFound when no row matches.
func FindIdentityByEmail(ctx context.Context, email string) (Identity, error) {
	email = normalizeEmail(email)
	var id Identity
	err := db.QueryRow(ctx, `
		SELECT id, email, email_verified_at, signup_ip_hash, signup_ua_hash, created_at, updated_at
		FROM api_identities WHERE email = $1`, email,
	).Scan(
		&id.ID, &id.Email, &id.EmailVerifiedAt,
		&id.SignupIPHash, &id.SignupUAHash,
		&id.CreatedAt, &id.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, ErrNotFound
		}
		return Identity{}, fmt.Errorf("signup.FindIdentityByEmail: %w", err)
	}
	return id, nil
}

// FindIdentityByID returns the identity for the given UUID string.
// Returns ErrNotFound when no row matches.
func FindIdentityByID(ctx context.Context, id string) (Identity, error) {
	var ident Identity
	err := db.QueryRow(ctx, `
		SELECT id, email, email_verified_at, signup_ip_hash, signup_ua_hash, created_at, updated_at
		FROM api_identities WHERE id = $1`, id,
	).Scan(
		&ident.ID, &ident.Email, &ident.EmailVerifiedAt,
		&ident.SignupIPHash, &ident.SignupUAHash,
		&ident.CreatedAt, &ident.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, ErrNotFound
		}
		return Identity{}, fmt.Errorf("signup.FindIdentityByID: %w", err)
	}
	return ident, nil
}

// MarkEmailVerified sets email_verified_at to now for the given identity id.
func MarkEmailVerified(ctx context.Context, identityID string) error {
	tag, err := db.Exec(ctx,
		`UPDATE api_identities SET email_verified_at = NOW() WHERE id = $1`, identityID)
	if err != nil {
		return fmt.Errorf("signup.MarkEmailVerified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Magic-link tokens ─────────────────────────────────────────────────────────

// Token represents a row in magic_link_tokens.
type Token struct {
	ID         string
	IdentityID string
	TokenHash  string
	ExpiresAt  time.Time
	UsedAt     *time.Time
	CreatedAt  time.Time
}

// HashToken returns the hex-encoded SHA-256 of the raw token string.
// The hash is what gets stored; the raw token is sent in the email link.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// CreateToken inserts a new magic_link_tokens row and returns it.
func CreateToken(ctx context.Context, identityID, rawToken string, expiresAt time.Time) (Token, error) {
	hash := HashToken(rawToken)
	var t Token
	err := db.QueryRow(ctx, `
		INSERT INTO magic_link_tokens (identity_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, identity_id, token_hash, expires_at, used_at, created_at`,
		identityID, hash, expiresAt,
	).Scan(&t.ID, &t.IdentityID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if err != nil {
		return Token{}, fmt.Errorf("signup.CreateToken: %w", err)
	}
	return t, nil
}

// ConsumeToken atomically marks a token as used and returns its identity_id.
// Returns ErrNotFound, ErrTokenUsed, or ErrTokenExpired as appropriate.
// "Consume" is the single gate for the verify endpoint — if this succeeds the
// caller may proceed to issue a key.
func ConsumeToken(ctx context.Context, rawToken string) (identityID string, err error) {
	hash := HashToken(rawToken)
	now := time.Now().UTC()

	var t Token
	err = db.QueryRow(ctx, `
		SELECT id, identity_id, token_hash, expires_at, used_at, created_at
		FROM magic_link_tokens WHERE token_hash = $1`, hash,
	).Scan(&t.ID, &t.IdentityID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("signup.ConsumeToken (fetch): %w", err)
	}

	if t.UsedAt != nil {
		return "", ErrTokenUsed
	}
	if now.After(t.ExpiresAt) {
		return "", ErrTokenExpired
	}

	// Mark used atomically. The unique constraint on token_hash prevents
	// concurrent verification races — only one UPDATE wins.
	tag, err := db.Exec(ctx,
		`UPDATE magic_link_tokens SET used_at = $1 WHERE id = $2 AND used_at IS NULL`,
		now, t.ID,
	)
	if err != nil {
		return "", fmt.Errorf("signup.ConsumeToken (mark used): %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Concurrent consume — treat as already used.
		return "", ErrTokenUsed
	}
	return t.IdentityID, nil
}

// CountRecentTokens returns the number of tokens issued for an identity
// within the given lookback window. Used by abuse controls.
func CountRecentTokens(ctx context.Context, identityID string, since time.Time) (int, error) {
	var n int
	err := db.QueryRow(ctx,
		`SELECT COUNT(*) FROM magic_link_tokens WHERE identity_id = $1 AND created_at >= $2`,
		identityID, since,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("signup.CountRecentTokens: %w", err)
	}
	return n, nil
}

// PruneExpiredTokens deletes all tokens whose expires_at is before cutoff.
// Intended to be run periodically (e.g. daily cron).
func PruneExpiredTokens(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := db.Exec(ctx,
		`DELETE FROM magic_link_tokens WHERE expires_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("signup.PruneExpiredTokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ── API key registry ──────────────────────────────────────────────────────────

// KeyRecord represents a row in api_keys_registry.
type KeyRecord struct {
	ID            string
	IdentityID    string
	ProviderKeyID string
	Status        string
	CreatedAt     time.Time
	RevokedAt     *time.Time
}

// RegisterKey inserts a new active key for an identity.
// If the identity already has an active key, the caller should revoke it first
// (RevokeKey) to satisfy the one-active-key-per-identity constraint.
func RegisterKey(ctx context.Context, identityID, providerKeyID string) (KeyRecord, error) {
	var k KeyRecord
	err := db.QueryRow(ctx, `
		INSERT INTO api_keys_registry (identity_id, provider_key_id, status)
		VALUES ($1, $2, 'active')
		RETURNING id, identity_id, provider_key_id, status, created_at, revoked_at`,
		identityID, providerKeyID,
	).Scan(&k.ID, &k.IdentityID, &k.ProviderKeyID, &k.Status, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("signup.RegisterKey: %w", err)
	}
	return k, nil
}

// FindActiveKey returns the active key record for an identity.
// Returns ErrNotFound if no active key exists.
func FindActiveKey(ctx context.Context, identityID string) (KeyRecord, error) {
	var k KeyRecord
	err := db.QueryRow(ctx, `
		SELECT id, identity_id, provider_key_id, status, created_at, revoked_at
		FROM api_keys_registry WHERE identity_id = $1 AND status = 'active'`, identityID,
	).Scan(&k.ID, &k.IdentityID, &k.ProviderKeyID, &k.Status, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return KeyRecord{}, ErrNotFound
		}
		return KeyRecord{}, fmt.Errorf("signup.FindActiveKey: %w", err)
	}
	return k, nil
}

// RevokeKey marks the given provider key as revoked.
// Returns ErrNotFound if the key doesn't exist or is already revoked.
func RevokeKey(ctx context.Context, providerKeyID string) error {
	tag, err := db.Exec(ctx, `
		UPDATE api_keys_registry
		SET status = 'revoked', revoked_at = NOW()
		WHERE provider_key_id = $1 AND status = 'active'`,
		providerKeyID)
	if err != nil {
		return fmt.Errorf("signup.RevokeKey: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// nullStr converts an empty string to nil so optional columns store NULL
// rather than an empty string.
func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
