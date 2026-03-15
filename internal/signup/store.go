// Package signup implements the store layer for the free-key-issuance epic.
// It covers api_identities, magic_link_tokens, and api_keys_registry.
package signup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("signup: record not found")

// ErrTokenConsumed is returned when a token has already been used.
var ErrTokenConsumed = errors.New("signup: token already used")

// ErrTokenExpired is returned when the token TTL has elapsed.
var ErrTokenExpired = errors.New("signup: token expired")

// ErrDuplicateActiveKey is returned when a second active key would violate the
// one-active-key-per-identity policy.
var ErrDuplicateActiveKey = errors.New("signup: identity already has an active key")

// ─── Domain types ─────────────────────────────────────────────────────────────

// Identity represents a row in api_identities.
type Identity struct {
	ID               string
	Email            string
	EmailVerifiedAt  *time.Time
	IPHash           string
	UAHash           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
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
	ProviderKeyID string // Unkey key.id
	Status        string // "active" | "revoked"
	CreatedAt     time.Time
	RevokedAt     *time.Time
}

// ─── Store interface ──────────────────────────────────────────────────────────

// Store abstracts all database access for the signup flow.
// The production implementation is pgxStore; tests use a mock.
type Store interface {
	// Identity operations
	UpsertIdentity(ctx context.Context, email, ipHash, uaHash string) (*Identity, error)
	GetIdentityByEmail(ctx context.Context, email string) (*Identity, error)
	GetIdentityByID(ctx context.Context, id string) (*Identity, error)
	MarkEmailVerified(ctx context.Context, identityID string) error

	// Token operations
	InsertToken(ctx context.Context, identityID, tokenHash string, expiresAt time.Time) (*MagicLinkToken, error)
	// ConsumeToken atomically finds the token by hash, validates it has not been
	// used and has not expired, marks it used, and returns the token record.
	// Returns ErrNotFound, ErrTokenConsumed, or ErrTokenExpired on failure.
	ConsumeToken(ctx context.Context, tokenHash string) (*MagicLinkToken, error)
	DeleteExpiredTokens(ctx context.Context) (int64, error)

	// Key registry operations
	GetActiveKey(ctx context.Context, identityID string) (*KeyRecord, error)
	InsertKey(ctx context.Context, identityID, providerKeyID string) (*KeyRecord, error)
	RevokeKey(ctx context.Context, identityID, providerKeyID string) error
	// RevokeAndInsertKey atomically revokes the old key and inserts the new one
	// in a single transaction. Returns the new KeyRecord. If oldProviderKeyID is
	// empty, only the insert is performed.
	RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*KeyRecord, error)
}

// ─── Production implementation ────────────────────────────────────────────────

type pgxStore struct {
	db *pgxpool.Pool
}

// NewStore returns a production Store backed by the given connection pool.
func NewStore(db *pgxpool.Pool) Store {
	return &pgxStore{db: db}
}

// ── Identity ──────────────────────────────────────────────────────────────────

// UpsertIdentity inserts a new identity for email (normalized to lower-case) or
// returns the existing one. ipHash and uaHash are set only on initial creation.
func (s *pgxStore) UpsertIdentity(ctx context.Context, email, ipHash, uaHash string) (*Identity, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO api_identities (email, ip_hash, ua_hash)
		VALUES (LOWER($1), $2, $3)
		ON CONFLICT ((LOWER(email))) DO UPDATE
			SET updated_at = NOW()
		RETURNING id, email, email_verified_at, ip_hash, ua_hash, created_at, updated_at`,
		email, nullStr(ipHash), nullStr(uaHash),
	)
	return scanIdentity(row)
}

// GetIdentityByEmail looks up an identity by normalized email.
func (s *pgxStore) GetIdentityByEmail(ctx context.Context, email string) (*Identity, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, email, email_verified_at, ip_hash, ua_hash, created_at, updated_at
		FROM api_identities WHERE LOWER(email) = LOWER($1)`,
		email,
	)
	id, err := scanIdentity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return id, err
}

// GetIdentityByID looks up an identity by UUID.
func (s *pgxStore) GetIdentityByID(ctx context.Context, id string) (*Identity, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, email, email_verified_at, ip_hash, ua_hash, created_at, updated_at
		FROM api_identities WHERE id = $1`,
		id,
	)
	ident, err := scanIdentity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ident, err
}

// MarkEmailVerified stamps email_verified_at = NOW() for an identity.
func (s *pgxStore) MarkEmailVerified(ctx context.Context, identityID string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE api_identities
		SET email_verified_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND email_verified_at IS NULL`,
		identityID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either not found or already verified — both are acceptable; idempotent.
		return nil
	}
	return nil
}

// ── Token ─────────────────────────────────────────────────────────────────────

// InsertToken creates a new magic-link token record.
func (s *pgxStore) InsertToken(ctx context.Context, identityID, tokenHash string, expiresAt time.Time) (*MagicLinkToken, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO magic_link_tokens (identity_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, identity_id, token_hash, expires_at, used_at, created_at`,
		identityID, tokenHash, expiresAt,
	)
	return scanToken(row)
}

// ConsumeToken atomically validates and marks a token as used.
func (s *pgxStore) ConsumeToken(ctx context.Context, tokenHash string) (*MagicLinkToken, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock row for update to prevent concurrent consumption.
	var t MagicLinkToken
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, identity_id, token_hash, expires_at, used_at, created_at
		FROM magic_link_tokens
		WHERE token_hash = $1
		FOR UPDATE`,
		tokenHash,
	).Scan(&t.ID, &t.IdentityID, &t.TokenHash, &t.ExpiresAt, &usedAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	t.UsedAt = usedAt
	if usedAt != nil {
		return nil, ErrTokenConsumed
	}
	if time.Now().After(t.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	now := time.Now()
	_, err = tx.Exec(ctx, `UPDATE magic_link_tokens SET used_at = $1 WHERE id = $2`, now, t.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	t.UsedAt = &now
	return &t, nil
}

// DeleteExpiredTokens removes tokens that expired and have not been consumed.
func (s *pgxStore) DeleteExpiredTokens(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM magic_link_tokens
		WHERE expires_at < NOW() AND used_at IS NULL`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── Key registry ──────────────────────────────────────────────────────────────

// GetActiveKey returns the active key for an identity, or ErrNotFound.
func (s *pgxStore) GetActiveKey(ctx context.Context, identityID string) (*KeyRecord, error) {
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
func (s *pgxStore) InsertKey(ctx context.Context, identityID, providerKeyID string) (*KeyRecord, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO api_keys_registry (identity_id, provider_key_id)
		VALUES ($1, $2)
		RETURNING id, identity_id, provider_key_id, status, created_at, revoked_at`,
		identityID, providerKeyID,
	)
	k, err := scanKey(row)
	if err != nil {
		// Unique partial index violation → duplicate active key.
		if isPgUniqueViolation(err) {
			return nil, ErrDuplicateActiveKey
		}
		return nil, err
	}
	return k, nil
}

// RevokeKey marks the given Unkey key ID as revoked for the identity.
func (s *pgxStore) RevokeKey(ctx context.Context, identityID, providerKeyID string) error {
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

// RevokeAndInsertKey atomically revokes the old key and inserts the new one.
func (s *pgxStore) RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*KeyRecord, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Revoke old key if specified.
	if oldProviderKeyID != "" {
		tag, err := tx.Exec(ctx, `
			UPDATE api_keys_registry
			SET status = 'revoked', revoked_at = NOW()
			WHERE identity_id = $1 AND provider_key_id = $2 AND status = 'active'`,
			identityID, oldProviderKeyID,
		)
		if err != nil {
			return nil, fmt.Errorf("revoke old key: %w", err)
		}
		if tag.RowsAffected() == 0 {
			// Old key already revoked or missing — not fatal for regeneration.
		}
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

// ─── Scan helpers ─────────────────────────────────────────────────────────────

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

// ─── Helpers ──────────────────────────────────────────────────────────────────

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
	return errors.Is(err, errUniqueViolation{}) ||
		containsSQLState(err, "23505")
}

type errUniqueViolation struct{}

func (errUniqueViolation) Error() string { return "unique violation" }
func (errUniqueViolation) Is(err error) bool {
	type pgErr interface{ SQLState() string }
	if pe, ok := err.(pgErr); ok {
		return pe.SQLState() == "23505"
	}
	return false
}

func containsSQLState(err error, code string) bool {
	type sqlStater interface{ SQLState() string }
	var target sqlStater
	if errors.As(err, &target) {
		return target.SQLState() == code
	}
	return false
}
