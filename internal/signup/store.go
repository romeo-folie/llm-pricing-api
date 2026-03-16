// Package signup provides the data-access layer for the free API-key
// onboarding flow: email identity management, magic-link token lifecycle,
// and the Unkey-backed API key registry.
//
// Use NewStore to obtain a Store from a *pgxpool.Pool. All operations are
// safe for concurrent use; there is no package-level state.
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

// ErrTokenConsumed is returned when a magic-link token has already been consumed.
var ErrTokenConsumed = errors.New("signup: token already used")

// ErrTokenExpired is returned when a magic-link token is past its expires_at.
var ErrTokenExpired = errors.New("signup: token expired")

// ErrDuplicateActiveKey is returned when an identity already has an active key
// and a second insertion is attempted.
var ErrDuplicateActiveKey = errors.New("signup: identity already has an active key")

// ── Store interface ───────────────────────────────────────────────────────────

// Store abstracts all database access for the signup flow.
// The production implementation is PgxStore; tests use an in-memory mock.
type Store interface {
	// Identity operations
	UpsertIdentity(ctx context.Context, email, ipHash, uaHash string) (*Identity, error)
	GetIdentityByEmail(ctx context.Context, email string) (*Identity, error)
	GetIdentityByID(ctx context.Context, id string) (*Identity, error)
	MarkEmailVerified(ctx context.Context, identityID string) error

	// Token operations
	InsertToken(ctx context.Context, identityID, tokenHash string, expiresAt time.Time) (*MagicLinkToken, error)
	ConsumeToken(ctx context.Context, tokenHash string) (*MagicLinkToken, error)
	DeleteExpiredTokens(ctx context.Context) (int64, error)

	// Key registry operations
	GetActiveKey(ctx context.Context, identityID string) (*KeyRecord, error)
	InsertKey(ctx context.Context, identityID, providerKeyID string) (*KeyRecord, error)
	RevokeKey(ctx context.Context, identityID, providerKeyID string) error
	RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*KeyRecord, error)
}

// ── Domain types ──────────────────────────────────────────────────────────────

// Identity represents a row in api_identities.
type Identity struct {
	ID              string
	Email           string
	EmailVerifiedAt *time.Time
	IPHash          string
	UAHash          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MagicLinkToken represents a row in magic_link_tokens.
type MagicLinkToken struct {
	ID         string
	IdentityID string
	TokenHash  string
	ExpiresAt  time.Time
	UsedAt     *time.Time
	CreatedAt  time.Time
}

// KeyRecord represents a row in api_keys_registry.
type KeyRecord struct {
	ID            string
	IdentityID    string
	ProviderKeyID string
	Status        string
	CreatedAt     time.Time
	RevokedAt     *time.Time
}

// ── Production implementation ─────────────────────────────────────────────────

// PgxStore is the production Store backed by a pgx connection pool.
type PgxStore struct {
	db *pgxpool.Pool
}

// NewStore returns a production Store backed by the given connection pool.
func NewStore(db *pgxpool.Pool) Store {
	return &PgxStore{db: db}
}

// HashToken returns the hex-encoded SHA-256 of the raw token string.
// The hash is what gets stored; the raw token is sent in the email link.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

// ── Identity ──────────────────────────────────────────────────────────────────

// UpsertIdentity inserts a new identity for email (normalized to lower-case) or
// returns the existing one. ipHash and uaHash are set only on initial creation.
// Returns an error if the normalized email is empty.
func (s *PgxStore) UpsertIdentity(ctx context.Context, email, ipHash, uaHash string) (*Identity, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("signup.UpsertIdentity: email must not be empty")
	}
	row := s.db.QueryRow(ctx, `
		INSERT INTO api_identities (email, signup_ip_hash, signup_ua_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET updated_at = NOW()
		RETURNING id, email, email_verified_at, signup_ip_hash, signup_ua_hash, created_at, updated_at`,
		email, nullStr(ipHash), nullStr(uaHash),
	)
	return scanIdentity(row)
}

// GetIdentityByEmail returns the identity for the given (normalized) email.
// Returns ErrNotFound when no row matches.
func (s *PgxStore) GetIdentityByEmail(ctx context.Context, email string) (*Identity, error) {
	email = normalizeEmail(email)
	row := s.db.QueryRow(ctx, `
		SELECT id, email, email_verified_at, signup_ip_hash, signup_ua_hash, created_at, updated_at
		FROM api_identities WHERE email = $1`, email,
	)
	id, err := scanIdentity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return id, err
}

// GetIdentityByID returns the identity for the given UUID string.
// Returns ErrNotFound when no row matches.
func (s *PgxStore) GetIdentityByID(ctx context.Context, id string) (*Identity, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, email, email_verified_at, signup_ip_hash, signup_ua_hash, created_at, updated_at
		FROM api_identities WHERE id = $1`, id,
	)
	ident, err := scanIdentity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ident, err
}

// MarkEmailVerified stamps email_verified_at = NOW() for an identity.
// The update is idempotent: COALESCE preserves an existing verified timestamp.
// Returns ErrNotFound when no identity with the given ID exists.
func (s *PgxStore) MarkEmailVerified(ctx context.Context, identityID string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE api_identities
		SET email_verified_at = COALESCE(email_verified_at, NOW()), updated_at = NOW()
		WHERE id = $1`,
		identityID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Token ─────────────────────────────────────────────────────────────────────

// InsertToken creates a new magic-link token record with a pre-hashed token.
func (s *PgxStore) InsertToken(ctx context.Context, identityID, tokenHash string, expiresAt time.Time) (*MagicLinkToken, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("signup.InsertToken: tokenHash must not be empty")
	}
	row := s.db.QueryRow(ctx, `
		INSERT INTO magic_link_tokens (identity_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, identity_id, token_hash, expires_at, used_at, created_at`,
		identityID, tokenHash, expiresAt,
	)
	return scanToken(row)
}

// ConsumeToken atomically validates and marks a token as used.
// Returns ErrNotFound, ErrTokenConsumed, or ErrTokenExpired as appropriate.
//
// Uses FOR UPDATE row locking to prevent concurrent consumption. Expiry is
// checked against the DB server clock (NOW()) to stay consistent with
// DeleteExpiredTokens and avoid app/DB clock skew issues.
func (s *PgxStore) ConsumeToken(ctx context.Context, tokenHash string) (*MagicLinkToken, error) {
	if tokenHash == "" {
		return nil, ErrNotFound
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock row for update to prevent concurrent consumption.
	// Fetch NOW() from the DB so expiry checks use the same clock as
	// DeleteExpiredTokens and other server-side time comparisons.
	var t MagicLinkToken
	var serverNow time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, identity_id, token_hash, expires_at, used_at, created_at, NOW()
		FROM magic_link_tokens
		WHERE token_hash = $1
		FOR UPDATE`,
		tokenHash,
	).Scan(&t.ID, &t.IdentityID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt, &serverNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if t.UsedAt != nil {
		return nil, ErrTokenConsumed
	}
	if serverNow.After(t.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	_, err = tx.Exec(ctx, `UPDATE magic_link_tokens SET used_at = NOW() WHERE id = $1`, t.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	t.UsedAt = &serverNow
	return &t, nil
}

// DeleteExpiredTokens removes tokens that expired and have not been consumed.
func (s *PgxStore) DeleteExpiredTokens(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM magic_link_tokens
		WHERE expires_at <= NOW() AND used_at IS NULL`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── Key registry ──────────────────────────────────────────────────────────────

// GetActiveKey returns the active key for an identity, or ErrNotFound.
func (s *PgxStore) GetActiveKey(ctx context.Context, identityID string) (*KeyRecord, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, identity_id, provider_key_id, status, created_at, revoked_at
		FROM api_keys_registry
		WHERE identity_id = $1 AND status = 'active'`,
		identityID,
	)
	k, err := scanKey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

// InsertKey creates a new active key record. Returns ErrDuplicateActiveKey if
// the identity already has an active key (enforced by the unique partial index).
// Returns an error if providerKeyID is empty or blank.
func (s *PgxStore) InsertKey(ctx context.Context, identityID, providerKeyID string) (*KeyRecord, error) {
	providerKeyID = strings.TrimSpace(providerKeyID)
	if providerKeyID == "" {
		return nil, fmt.Errorf("signup.InsertKey: providerKeyID must not be empty")
	}
	row := s.db.QueryRow(ctx, `
		INSERT INTO api_keys_registry (identity_id, provider_key_id)
		VALUES ($1, $2)
		RETURNING id, identity_id, provider_key_id, status, created_at, revoked_at`,
		identityID, providerKeyID,
	)
	k, err := scanKey(row)
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, ErrDuplicateActiveKey
		}
		return nil, err
	}
	return k, nil
}

// RevokeKey marks the given Unkey key ID as revoked for the identity.
// Returns an error if providerKeyID is empty or blank.
func (s *PgxStore) RevokeKey(ctx context.Context, identityID, providerKeyID string) error {
	providerKeyID = strings.TrimSpace(providerKeyID)
	if providerKeyID == "" {
		return fmt.Errorf("signup.RevokeKey: providerKeyID must not be empty")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE api_keys_registry
		SET status = 'revoked', revoked_at = NOW()
		WHERE identity_id = $1 AND provider_key_id = $2 AND status = 'active'`,
		identityID, providerKeyID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAndInsertKey atomically revokes the old key and inserts the new one
// in a single transaction. Returns the new KeyRecord. If oldProviderKeyID is
// empty, only the insert is performed.
func (s *PgxStore) RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*KeyRecord, error) {
	newProviderKeyID = strings.TrimSpace(newProviderKeyID)
	if newProviderKeyID == "" {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: newProviderKeyID must not be empty")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Revoke old key if specified.
	if oldProviderKeyID != "" {
		_, err := tx.Exec(ctx, `
			UPDATE api_keys_registry
			SET status = 'revoked', revoked_at = NOW()
			WHERE identity_id = $1 AND provider_key_id = $2 AND status = 'active'`,
			identityID, oldProviderKeyID,
		)
		if err != nil {
			return nil, fmt.Errorf("revoke old key: %w", err)
		}
		// Old key already revoked or missing is not fatal for regeneration.
	}

	// Insert new key.
	row := tx.QueryRow(ctx, `
		INSERT INTO api_keys_registry (identity_id, provider_key_id)
		VALUES ($1, $2)
		RETURNING id, identity_id, provider_key_id, status, created_at, revoked_at`,
		identityID, newProviderKeyID,
	)
	k, err := scanKey(row)
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, ErrDuplicateActiveKey
		}
		return nil, fmt.Errorf("insert new key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return k, nil
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanIdentity(row rowScanner) (*Identity, error) {
	var id Identity
	var ipHash, uaHash *string
	err := row.Scan(
		&id.ID, &id.Email, &id.EmailVerifiedAt,
		&ipHash, &uaHash,
		&id.CreatedAt, &id.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if ipHash != nil {
		id.IPHash = *ipHash
	}
	if uaHash != nil {
		id.UAHash = *uaHash
	}
	return &id, nil
}

func scanToken(row rowScanner) (*MagicLinkToken, error) {
	var t MagicLinkToken
	err := row.Scan(&t.ID, &t.IdentityID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanKey(row rowScanner) (*KeyRecord, error) {
	var k KeyRecord
	err := row.Scan(&k.ID, &k.IdentityID, &k.ProviderKeyID, &k.Status, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isPgUniqueViolation detects PostgreSQL unique constraint violations (SQLSTATE 23505).
func isPgUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type sqlStater interface{ SQLState() string }
	var target sqlStater
	if errors.As(err, &target) {
		return target.SQLState() == "23505"
	}
	return false
}
