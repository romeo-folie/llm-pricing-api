// Package signup provides the data-access layer for the free API-key
// onboarding flow: email identity management, magic-link token lifecycle,
// the Unkey-backed API key registry, and the agent device-grant records that
// let an agent collect a key on a user's behalf.
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

// MaxActiveKeysPerIdentity caps how many keys one identity may hold at once.
// Keys are per-agent so each can be revoked and attributed independently; the
// cap bounds how far a single account can fan out. Mirrors the 5-webhook cap
// and, like it, is enforced under a lock rather than by an unguarded
// read-then-write count (which races).
const MaxActiveKeysPerIdentity = 5

// Provenance values for KeyRecord.CreatedVia. These mirror the CHECK constraint
// on api_keys_registry.created_via added by migration 000019.
const (
	CreatedViaMagicLink = "magic_link"
	CreatedViaAgent     = "agent"
)

// maxKeyLabelBytes bounds KeyRecord.Label. Mirrors the DB CHECK constraint so
// the handler gets a clear error instead of a constraint violation.
const maxKeyLabelBytes = 64

// ErrActiveKeyLimitReached is returned when an identity already holds
// MaxActiveKeysPerIdentity active keys.
var ErrActiveKeyLimitReached = errors.New("signup: active key limit reached")

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
	ListActiveKeys(ctx context.Context, identityID string) ([]KeyRecord, error)
	InsertKey(ctx context.Context, identityID, providerKeyID string) (*KeyRecord, error)
	InsertKeyWithLabel(ctx context.Context, identityID, providerKeyID, label, createdVia string) (*KeyRecord, error)
	RevokeKey(ctx context.Context, identityID, providerKeyID string) error
	// RevokeKeyByID revokes one key by its registry ID and returns the Unkey
	// provider key ID. alreadyRevoked reports a repeat call, so the caller can
	// retry an upstream revocation that previously failed.
	RevokeKeyByID(ctx context.Context, identityID, keyID string) (providerKeyID string, alreadyRevoked bool, err error)
	RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*KeyRecord, error)

	// Agent device-grant operations. CreateAgentGrant generates both codes
	// internally and returns the raw device code, which is the only moment it
	// exists in plaintext.
	CreateAgentGrant(ctx context.Context, clientName, platform string, expiresAt time.Time) (*AgentGrant, string, error)
	GetAgentGrantByDeviceCode(ctx context.Context, rawDeviceCode string) (*AgentGrant, error)
	GetAgentGrantByUserCode(ctx context.Context, userCode string) (*AgentGrant, error)
	DecideAgentGrant(ctx context.Context, userCode, identityID string, approve bool) (*AgentGrant, error)
	RedeemAgentGrant(ctx context.Context, rawDeviceCode string) (*AgentGrant, error)
	RevertAgentGrantRedemption(ctx context.Context, grantID string) error
	DeleteExpiredAgentGrants(ctx context.Context) (int64, error)
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
	// Label is the human-readable owner of the key — an agent's self-reported
	// client name for agent-issued keys, empty for magic-link keys.
	Label string
	// CreatedVia is 'magic_link' or 'agent'.
	CreatedVia string
	Status     string
	CreatedAt  time.Time
	RevokedAt  *time.Time
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
	ident, err := scanIdentity(row)
	if err != nil {
		return nil, fmt.Errorf("signup.UpsertIdentity: %w", err)
	}
	return ident, nil
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
	if err != nil {
		return nil, fmt.Errorf("signup.GetIdentityByEmail: %w", err)
	}
	return id, nil
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
	if err != nil {
		return nil, fmt.Errorf("signup.GetIdentityByID: %w", err)
	}
	return ident, nil
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
		return fmt.Errorf("signup.MarkEmailVerified: %w", err)
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
	result, err := scanToken(row)
	if err != nil {
		return nil, fmt.Errorf("signup.InsertToken: %w", err)
	}
	return result, nil
}

// ConsumeToken atomically validates and marks a token as used.
// Returns ErrNotFound, ErrTokenConsumed, or ErrTokenExpired as appropriate.
//
// Uses FOR UPDATE row locking to prevent concurrent consumption. Expiry is
// checked against clock_timestamp() (wall-clock time) rather than NOW()
// (transaction-start time) so that a token cannot be consumed after real
// expiry when the transaction blocks on the FOR UPDATE lock.
func (s *PgxStore) ConsumeToken(ctx context.Context, tokenHash string) (*MagicLinkToken, error) {
	if tokenHash == "" {
		return nil, ErrNotFound
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("signup.ConsumeToken: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock row for update to prevent concurrent consumption.
	// Fetch clock_timestamp() (real wall-clock time) instead of NOW()
	// (transaction-start time) so that if the FOR UPDATE lock blocks
	// until after expiry, the check correctly rejects the token.
	var t MagicLinkToken
	var serverNow time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, identity_id, token_hash, expires_at, used_at, created_at, clock_timestamp()
		FROM magic_link_tokens
		WHERE token_hash = $1
		FOR UPDATE`,
		tokenHash,
	).Scan(&t.ID, &t.IdentityID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt, &serverNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("signup.ConsumeToken: query: %w", err)
	}

	if t.UsedAt != nil {
		return nil, ErrTokenConsumed
	}
	if !serverNow.Before(t.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	var usedAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE magic_link_tokens SET used_at = clock_timestamp()
		WHERE id = $1 AND used_at IS NULL AND expires_at > clock_timestamp()
		RETURNING used_at`, t.ID).Scan(&usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Row was locked but state changed between SELECT and UPDATE
		// (e.g. token expired in the interim). Re-check to disambiguate.
		var refreshUsedAt *time.Time
		var refreshExpiry time.Time
		disambigErr := tx.QueryRow(ctx, `SELECT used_at, expires_at FROM magic_link_tokens WHERE id = $1`, t.ID).
			Scan(&refreshUsedAt, &refreshExpiry)
		if disambigErr != nil {
			if errors.Is(disambigErr, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("signup.ConsumeToken: disambiguation query: %w", disambigErr)
		}
		if refreshUsedAt != nil {
			return nil, ErrTokenConsumed
		}
		return nil, ErrTokenExpired
	}
	if err != nil {
		return nil, fmt.Errorf("signup.ConsumeToken: mark used: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("signup.ConsumeToken: commit: %w", err)
	}
	t.UsedAt = &usedAt
	return &t, nil
}

// DeleteExpiredTokens removes tokens that expired and have not been consumed.
func (s *PgxStore) DeleteExpiredTokens(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM magic_link_tokens
		WHERE expires_at <= NOW() AND used_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("signup.DeleteExpiredTokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ── Key registry ──────────────────────────────────────────────────────────────

// GetActiveKey returns the most recently created active key for an identity,
// or ErrNotFound. With multiple keys per identity this is a convenience for
// callers that only need "is there one?" — use ListActiveKeys when the
// specific key matters. Ordered so the result is deterministic rather than
// whichever row the planner happens to return first.
func (s *PgxStore) GetActiveKey(ctx context.Context, identityID string) (*KeyRecord, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, identity_id, provider_key_id, label, created_via, status, created_at, revoked_at
		FROM api_keys_registry
		WHERE identity_id = $1 AND status = 'active'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		identityID,
	)
	k, err := scanKey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("signup.GetActiveKey: %w", err)
	}
	return k, nil
}

// ListActiveKeys returns every active key for an identity, newest first.
// Returns an empty slice (not ErrNotFound) when the identity has no keys, so
// callers can render an empty list without special-casing the error.
func (s *PgxStore) ListActiveKeys(ctx context.Context, identityID string) ([]KeyRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, identity_id, provider_key_id, label, created_via, status, created_at, revoked_at
		FROM api_keys_registry
		WHERE identity_id = $1 AND status = 'active'
		ORDER BY created_at DESC, id DESC`,
		identityID,
	)
	if err != nil {
		return nil, fmt.Errorf("signup.ListActiveKeys: %w", err)
	}
	defer rows.Close()

	keys := make([]KeyRecord, 0, 1)
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, fmt.Errorf("signup.ListActiveKeys: scan: %w", err)
		}
		keys = append(keys, *k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("signup.ListActiveKeys: rows: %w", err)
	}
	return keys, nil
}

// InsertKey creates a new active key record with no label, as a magic-link key.
// Returns ErrActiveKeyLimitReached when the identity already holds
// MaxActiveKeysPerIdentity active keys.
func (s *PgxStore) InsertKey(ctx context.Context, identityID, providerKeyID string) (*KeyRecord, error) {
	return s.InsertKeyWithLabel(ctx, identityID, providerKeyID, "", CreatedViaMagicLink)
}

// InsertKeyWithLabel creates a new active key record owned by label.
//
// The cap is enforced inside a transaction that first takes a row lock on the
// identity. The lock is what makes it race-free: two concurrent inserts for the
// same identity serialize on it, so both cannot observe an under-cap count and
// then both insert. An unguarded count-then-insert would permit exactly that.
//
// Returns ErrActiveKeyLimitReached when at capacity, ErrNotFound when the
// identity does not exist.
func (s *PgxStore) InsertKeyWithLabel(ctx context.Context, identityID, providerKeyID, label, createdVia string) (*KeyRecord, error) {
	providerKeyID = strings.TrimSpace(providerKeyID)
	if providerKeyID == "" {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: providerKeyID must not be empty")
	}
	label = strings.TrimSpace(label)
	if len(label) > maxKeyLabelBytes {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: label must be at most %d bytes", maxKeyLabelBytes)
	}
	switch createdVia {
	case "":
		createdVia = CreatedViaMagicLink
	case CreatedViaMagicLink, CreatedViaAgent:
		// valid as-is
	default:
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: invalid createdVia %q", createdVia)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize concurrent key inserts for this identity.
	var locked int
	err = tx.QueryRow(ctx, `SELECT 1 FROM api_identities WHERE id = $1 FOR UPDATE`, identityID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: lock identity: %w", err)
	}

	var active int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM api_keys_registry
		WHERE identity_id = $1 AND status = 'active'`, identityID).Scan(&active); err != nil {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: count active keys: %w", err)
	}
	if active >= MaxActiveKeysPerIdentity {
		return nil, ErrActiveKeyLimitReached
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO api_keys_registry (identity_id, provider_key_id, label, created_via)
		VALUES ($1, $2, $3, $4)
		RETURNING id, identity_id, provider_key_id, label, created_via, status, created_at, revoked_at`,
		identityID, providerKeyID, label, createdVia,
	)
	k, err := scanKey(row)
	if err != nil {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("signup.InsertKeyWithLabel: commit tx: %w", err)
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
		return fmt.Errorf("signup.RevokeKey: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeKeyByID marks the registry row identified by its own primary key as
// revoked and returns the Unkey provider key ID it held, which the caller needs
// in order to revoke the key at Unkey. Scoped by identityID so one identity
// cannot revoke another's key by guessing a UUID.
//
// alreadyRevoked reports whether the row was already revoked before this call.
// The lookup deliberately matches already-revoked rows so the caller can retry
// the upstream Unkey revocation: that step is separate and can fail, and on a
// retry the row is already revoked here. Without this, a retry would get
// ErrNotFound and the revocation could never be completed.
//
// Returns ErrNotFound only when no row with that id belongs to the identity.
func (s *PgxStore) RevokeKeyByID(ctx context.Context, identityID, keyID string) (providerKeyID string, alreadyRevoked bool, err error) {
	keyID = strings.TrimSpace(keyID)
	identityID = strings.TrimSpace(identityID)
	if keyID == "" || identityID == "" {
		return "", false, ErrNotFound
	}

	// The CTE captures the pre-update status under a row lock, so the caller
	// learns whether this call did the revoking. COALESCE preserves an existing
	// revoked_at rather than restamping it on a retry.
	var priorStatus string
	err = s.db.QueryRow(ctx, `
		WITH target AS (
			SELECT id, status FROM api_keys_registry
			WHERE id = $1 AND identity_id = $2
			FOR UPDATE
		)
		UPDATE api_keys_registry AS k
		SET status = 'revoked',
		    revoked_at = COALESCE(k.revoked_at, NOW())
		FROM target AS t
		WHERE k.id = t.id
		RETURNING k.provider_key_id, t.status`,
		keyID, identityID,
	).Scan(&providerKeyID, &priorStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrNotFound
	}
	if err != nil {
		// A malformed UUID reaches Postgres as a cast error rather than a
		// missing row, so it maps to "not found" instead of a 500. Any
		// authenticated caller can otherwise generate error-level log noise.
		if isInvalidTextRepresentation(err) {
			return "", false, ErrNotFound
		}
		return "", false, fmt.Errorf("signup.RevokeKeyByID: %w", err)
	}
	return providerKeyID, priorStatus != "active", nil
}

// RevokeAndInsertKey atomically revokes the old key and inserts the new one
// in a single transaction. Returns the new KeyRecord. If oldProviderKeyID is
// empty, only the insert is performed (subject to the cap).
//
// The cap is enforced here too, not just in InsertKeyWithLabel. Rotation is an
// insert, so leaving it unchecked would make MaxActiveKeysPerIdentity advisory:
// two concurrent rotations of the same target both revoke nothing (the second
// UPDATE matches zero rows, which is deliberately non-fatal) and both insert,
// leaving one extra key per racing pair. The same identity row lock that
// InsertKeyWithLabel takes closes that, and the revoke runs first so the
// replaced key is already excluded from the count.
func (s *PgxStore) RevokeAndInsertKey(ctx context.Context, identityID, oldProviderKeyID, newProviderKeyID string) (*KeyRecord, error) {
	newProviderKeyID = strings.TrimSpace(newProviderKeyID)
	if newProviderKeyID == "" {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: newProviderKeyID must not be empty")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Revoke old key first, so it no longer counts against the cap below.
	if oldProviderKeyID != "" {
		_, err := tx.Exec(ctx, `
			UPDATE api_keys_registry
			SET status = 'revoked', revoked_at = NOW()
			WHERE identity_id = $1 AND provider_key_id = $2 AND status = 'active'`,
			identityID, oldProviderKeyID,
		)
		if err != nil {
			return nil, fmt.Errorf("signup.RevokeAndInsertKey: revoke old key: %w", err)
		}
		// Old key already revoked or missing is not fatal for regeneration.
	}

	// Serialize concurrent key writes for this identity, then enforce the cap.
	var locked int
	err = tx.QueryRow(ctx, `SELECT 1 FROM api_identities WHERE id = $1 FOR UPDATE`, identityID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: lock identity: %w", err)
	}

	var active int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM api_keys_registry
		WHERE identity_id = $1 AND status = 'active'`, identityID).Scan(&active); err != nil {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: count active keys: %w", err)
	}
	if active >= MaxActiveKeysPerIdentity {
		return nil, ErrActiveKeyLimitReached
	}

	// Insert the replacement, inheriting the replaced key's label and
	// provenance: rotating an agent's key must not silently relabel it as a
	// magic-link key, which would corrupt attribution. oldProviderKeyID may be
	// empty, in which case both subqueries yield NULL and the defaults apply.
	row := tx.QueryRow(ctx, `
		INSERT INTO api_keys_registry (identity_id, provider_key_id, label, created_via)
		SELECT $1, $2,
		       COALESCE((SELECT k.label FROM api_keys_registry k
		                 WHERE k.identity_id = $1 AND k.provider_key_id = $3), ''),
		       COALESCE((SELECT k.created_via FROM api_keys_registry k
		                 WHERE k.identity_id = $1 AND k.provider_key_id = $3), 'magic_link')
		RETURNING id, identity_id, provider_key_id, label, created_via, status, created_at, revoked_at`,
		identityID, newProviderKeyID, oldProviderKeyID,
	)
	k, err := scanKey(row)
	if err != nil {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: insert new key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("signup.RevokeAndInsertKey: commit tx: %w", err)
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
	err := row.Scan(
		&k.ID, &k.IdentityID, &k.ProviderKeyID, &k.Label, &k.CreatedVia,
		&k.Status, &k.CreatedAt, &k.RevokedAt,
	)
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
