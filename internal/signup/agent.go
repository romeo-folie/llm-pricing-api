package signup

// agent.go — agent device-grant records backing the "an agent obtains a key on
// behalf of its user" flow (RFC 8628 shape):
//
//	POST /auth/agent/device   → CreateAgentGrant, returns the raw device code
//	GET  /auth/agent/grant    → GetAgentGrantByUserCode (approval screen)
//	POST /auth/agent/approve  → DecideAgentGrant
//	POST /auth/agent/token    → GetAgentGrantByDeviceCode (status) then
//	                            RedeemAgentGrant (collect the key, exactly once)
//
// Two codes exist per grant and they are deliberately not interchangeable:
//
//   - The device code is high-entropy, is returned to the agent at creation and
//     never again. It is the credential that redeems the key, so only its
//     SHA-256 is stored (HashToken) and the raw value must never be logged.
//   - The user code is short and human-typed, so it is protected by being
//     useless on its own: knowing it lets you approve into *your own* identity,
//     not read someone else's key. Guessing it is a nuisance, not a takeover.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// AgentGrantTTL is how long a device grant stays redeemable. Short by
	// design: the user is expected to approve while the agent is still polling.
	AgentGrantTTL = 10 * time.Minute

	// AgentPollInterval is the minimum interval between token-endpoint polls for
	// one device code, and the value advertised to the agent as `interval`.
	// Defined here so the throttle and the advertised value cannot drift apart.
	AgentPollInterval = 5 * time.Second

	// UserCodeLength is the number of Crockford base32 characters in a user code.
	UserCodeLength = 8

	// agentGrantCodeAttempts bounds the retry loop when a generated user code
	// collides with an existing row. A collision has probability ~2^-40 per
	// attempt, so this is effectively "never fails", not a real limit.
	agentGrantCodeAttempts = 5

	// maxClientNameBytes bounds the agent-supplied client name. Mirrors the DB
	// CHECK constraint (which counts characters; UTF-8 bytes are the stricter
	// bound, which is the safe direction).
	maxClientNameBytes = 64

	// maxPlatformBytes bounds the agent-supplied platform string.
	maxPlatformBytes = 32

	// crockfordAlphabet omits I, L, O and U: I/L are indistinguishable from 1
	// and O from 0 when read off a terminal, and dropping U avoids spelling
	// words. Matches the agent_grants_user_code_format CHECK constraint.
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// userCodePattern mirrors the agent_grants_user_code_format CHECK constraint.
var userCodePattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{8}$`)

// Sentinels describing why a grant could not be redeemed. The token endpoint
// maps these onto its polling response rather than treating them as errors.
var (
	// ErrGrantPending means the user has not decided yet — keep polling.
	ErrGrantPending = errors.New("signup: grant pending approval")
	// ErrGrantDenied means the user explicitly refused the request.
	ErrGrantDenied = errors.New("signup: grant denied")
	// ErrGrantExpired means the grant's TTL elapsed before redemption.
	ErrGrantExpired = errors.New("signup: grant expired")
	// ErrGrantRedeemed means the key was already collected. The plaintext is
	// never served twice.
	ErrGrantRedeemed = errors.New("signup: grant already redeemed")
)

// AgentGrant represents a row in agent_grants.
type AgentGrant struct {
	ID             string
	DeviceCodeHash string
	UserCode       string
	ClientName     string
	Platform       string
	// IdentityID is set only once a signed-in user approves the grant.
	IdentityID *string
	Status     string
	ExpiresAt  time.Time
	DecidedAt  *time.Time
	RedeemedAt *time.Time
	CreatedAt  time.Time
}

// agentGrantColumns is the shared SELECT/RETURNING list, kept in one place so a
// new column cannot be added to the struct and forgotten in one of the queries.
const agentGrantColumns = `id, device_code_hash, user_code, client_name, platform,
	identity_id, status, expires_at, decided_at, redeemed_at, created_at`

// ── Code generation ───────────────────────────────────────────────────────────

// NewUserCode returns a random Crockford base32 code of UserCodeLength chars.
//
// Rejection sampling is unnecessary: 256 is an exact multiple of the 32-symbol
// alphabet, so reducing each random byte modulo 32 is already uniform.
func NewUserCode() (string, error) {
	b := make([]byte, UserCodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signup: generate user code: %w", err)
	}
	out := make([]byte, UserCodeLength)
	for i, v := range b {
		out[i] = crockfordAlphabet[int(v)%len(crockfordAlphabet)]
	}
	return string(out), nil
}

// NewDeviceCode returns a 256-bit raw device code. Only HashToken of this value
// is persisted; the raw string is handed to the agent once and is the sole
// credential that redeems the key.
func NewDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signup: generate device code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NormalizeUserCode canonicalises a user-typed code: it strips separators,
// upper-cases, and folds the characters Crockford defines as aliases (I and L
// to 1, O to 0). Callers must still validate the result with ValidUserCode.
func NormalizeUserCode(raw string) string {
	var sb strings.Builder
	sb.Grow(UserCodeLength)
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch r {
		case '-', ' ', '\t', '_':
			// Separators are display-only.
		case 'I', 'L':
			sb.WriteByte('1')
		case 'O':
			sb.WriteByte('0')
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ValidUserCode reports whether s is a well-formed (already normalised) code.
func ValidUserCode(s string) bool {
	return userCodePattern.MatchString(s)
}

// ValidateAgentClientName checks an agent's self-reported client name.
//
// The name is echoed verbatim on the user's approval screen and in the agent's
// own terminal output, so control characters are rejected outright: an embedded
// escape sequence is an injection vector in both places, not a rendering
// curiosity. Exported so the HTTP layer can answer 400 before touching the
// database.
func ValidateAgentClientName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("client_name is required")
	}
	if len(name) > maxClientNameBytes {
		return fmt.Errorf("client_name must be at most %d bytes", maxClientNameBytes)
	}
	if !isControlFree(name) {
		return errors.New("client_name must not contain control characters")
	}
	return nil
}

// ValidateAgentPlatform checks an agent's self-reported platform string.
// It is display-only, so it is length- and control-character-checked but
// otherwise unconstrained.
func ValidateAgentPlatform(platform string) error {
	platform = strings.TrimSpace(platform)
	if len(platform) > maxPlatformBytes {
		return fmt.Errorf("platform must be at most %d bytes", maxPlatformBytes)
	}
	if !isControlFree(platform) {
		return errors.New("platform must not contain control characters")
	}
	return nil
}

// isControlFree reports whether s contains only characters that are safe to
// echo back to a human.
//
// unicode.IsControl covers C0/C1 only, which is not enough here. Category Cf
// includes the bidirectional overrides (U+202E) and zero-width characters
// (U+200B) — not "control" characters, but equally able to make a consent
// screen read differently than it is, and to make two distinct agent names look
// identical in a key list. Zl/Zp are line/paragraph separators that can forge a
// line break in log output. Private-use characters are permitted: they are not
// reordering or invisible-by-default, and rejecting them would break legitimate
// icon-font glyphs in a client name.
func isControlFree(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Cs, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}

// FormatUserCode renders a stored code for display as XXXX-XXXX, the grouping
// users are shown on the approval screen and can type back.
func FormatUserCode(code string) string {
	if len(code) != UserCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// ── Store operations ──────────────────────────────────────────────────────────

// CreateAgentGrant mints a device grant, persisting only the SHA-256 of the
// device code, and returns the raw device code to the caller exactly once.
//
// Retries on user-code collision (see agentGrantCodeAttempts). Whether agents
// are allowed to request a grant at all is a handler/config concern, not a
// data-layer one — this method always mints.
func (s *PgxStore) CreateAgentGrant(ctx context.Context, clientName, platform string, expiresAt time.Time) (*AgentGrant, string, error) {
	clientName = strings.TrimSpace(clientName)
	if err := ValidateAgentClientName(clientName); err != nil {
		return nil, "", fmt.Errorf("signup.CreateAgentGrant: %w", err)
	}
	platform = strings.TrimSpace(platform)
	if err := ValidateAgentPlatform(platform); err != nil {
		return nil, "", fmt.Errorf("signup.CreateAgentGrant: %w", err)
	}

	for attempt := 0; attempt < agentGrantCodeAttempts; attempt++ {
		userCode, err := NewUserCode()
		if err != nil {
			return nil, "", err
		}
		rawDeviceCode, err := NewDeviceCode()
		if err != nil {
			return nil, "", err
		}

		row := s.db.QueryRow(ctx, `
			INSERT INTO agent_grants (device_code_hash, user_code, client_name, platform, expires_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING `+agentGrantColumns,
			HashToken(rawDeviceCode), userCode, clientName, platform, expiresAt,
		)
		g, err := scanAgentGrant(row)
		if err == nil {
			return g, rawDeviceCode, nil
		}
		// A user-code collision is retryable; a device-code-hash collision is
		// not realistically reachable and is surfaced as a real error.
		if isUniqueViolation(err, "agent_grants_user_code_unique") {
			continue
		}
		return nil, "", fmt.Errorf("signup.CreateAgentGrant: %w", err)
	}
	return nil, "", errors.New("signup.CreateAgentGrant: could not allocate a unique user code")
}

// GetAgentGrantByDeviceCode returns the grant for a raw device code.
// The raw code is hashed before lookup; the hash is what is stored.
// Returns ErrNotFound when no grant matches.
func (s *PgxStore) GetAgentGrantByDeviceCode(ctx context.Context, rawDeviceCode string) (*AgentGrant, error) {
	if strings.TrimSpace(rawDeviceCode) == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRow(ctx, `
		SELECT `+agentGrantColumns+`
		FROM agent_grants WHERE device_code_hash = $1`,
		HashToken(rawDeviceCode),
	)
	g, err := scanAgentGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("signup.GetAgentGrantByDeviceCode: %w", err)
	}
	return g, nil
}

// GetAgentGrantByUserCode returns the grant a user is being asked to approve.
// The code must already be normalised with NormalizeUserCode.
func (s *PgxStore) GetAgentGrantByUserCode(ctx context.Context, userCode string) (*AgentGrant, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+agentGrantColumns+`
		FROM agent_grants WHERE user_code = $1`,
		userCode,
	)
	g, err := scanAgentGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("signup.GetAgentGrantByUserCode: %w", err)
	}
	return g, nil
}

// DecideAgentGrant records a user's approve/deny decision on a grant.
//
// The decision is applied in a single guarded UPDATE, so a grant cannot be
// decided twice (or decided after expiry) by a concurrent pair of requests.
// identity_id is bound only on approval: a denial must not record which account
// was signed in when it happened.
//
// Returns ErrNotFound if no pending, unexpired grant has that code, or
// ErrGrantExpired / ErrGrantRedeemed if the row exists but is no longer decidable.
func (s *PgxStore) DecideAgentGrant(ctx context.Context, userCode, identityID string, approve bool) (*AgentGrant, error) {
	row := s.db.QueryRow(ctx, `
		UPDATE agent_grants
		SET status      = CASE WHEN $3 THEN 'approved' ELSE 'denied' END,
		    identity_id = CASE WHEN $3 THEN $2::uuid ELSE NULL END,
		    decided_at  = clock_timestamp()
		WHERE user_code = $1
		  AND status = 'pending'
		  AND expires_at > clock_timestamp()
		RETURNING `+agentGrantColumns,
		userCode, identityID, approve,
	)
	g, err := scanAgentGrant(row)
	if err == nil {
		return g, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("signup.DecideAgentGrant: %w", err)
	}

	// No row updated — find out why so the caller can render the right message.
	existing, lookupErr := s.GetAgentGrantByUserCode(ctx, userCode)
	if lookupErr != nil {
		return nil, lookupErr
	}
	switch existing.Status {
	case "redeemed":
		return nil, ErrGrantRedeemed
	case "approved", "denied":
		// Already decided; treat as not-found for decision purposes.
		return nil, ErrNotFound
	default:
		if !existing.ExpiresAt.After(time.Now()) {
			return nil, ErrGrantExpired
		}
		return nil, ErrNotFound
	}
}

// RedeemAgentGrant atomically consumes an approved grant and returns it, so the
// caller can issue the key. Exactly one concurrent caller can succeed: the
// status transition to 'redeemed' is the claim, and the key plaintext is never
// served twice.
//
// Returns ErrGrantPending / ErrGrantDenied / ErrGrantExpired / ErrGrantRedeemed
// for the corresponding terminal states, or ErrNotFound for an unknown code.
func (s *PgxStore) RedeemAgentGrant(ctx context.Context, rawDeviceCode string) (*AgentGrant, error) {
	if strings.TrimSpace(rawDeviceCode) == "" {
		return nil, ErrNotFound
	}
	deviceCodeHash := HashToken(rawDeviceCode)

	row := s.db.QueryRow(ctx, `
		UPDATE agent_grants
		SET status = 'redeemed', redeemed_at = clock_timestamp()
		WHERE device_code_hash = $1
		  AND status = 'approved'
		  AND redeemed_at IS NULL
		  AND expires_at > clock_timestamp()
		RETURNING `+agentGrantColumns,
		deviceCodeHash,
	)
	g, err := scanAgentGrant(row)
	if err == nil {
		return g, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("signup.RedeemAgentGrant: %w", err)
	}

	// Nothing to claim — disambiguate the state for the polling response.
	existing, lookupErr := s.GetAgentGrantByDeviceCode(ctx, rawDeviceCode)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if !existing.ExpiresAt.After(time.Now()) {
		return nil, ErrGrantExpired
	}
	switch existing.Status {
	case "pending":
		return nil, ErrGrantPending
	case "denied":
		return nil, ErrGrantDenied
	case "redeemed":
		return nil, ErrGrantRedeemed
	case "expired":
		return nil, ErrGrantExpired
	default:
		// 'approved' but not redeemable implies it expired between the UPDATE
		// and the lookup; treated as expired.
		return nil, ErrGrantExpired
	}
}

// RevertAgentGrantRedemption returns a redeemed grant to the approved state so
// the agent can retry, used as a compensating action when key issuance fails
// after the grant was claimed. Without it a transient Unkey failure would
// permanently strand the grant: the agent's next poll would see 'redeemed' and
// give up, having never received a key.
//
// Scoped to rows that are currently redeemed, so it cannot resurrect a grant
// that was never claimed. Returns ErrNotFound when no such row exists — callers
// treat that as "nothing to compensate".
func (s *PgxStore) RevertAgentGrantRedemption(ctx context.Context, grantID string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE agent_grants
		SET status = 'approved', redeemed_at = NULL
		WHERE id = $1 AND status = 'redeemed'`,
		grantID,
	)
	if err != nil {
		return fmt.Errorf("signup.RevertAgentGrantRedemption: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteExpiredAgentGrants removes grants whose TTL has elapsed, mirroring
// DeleteExpiredTokens. Returns the number of rows deleted.
func (s *PgxStore) DeleteExpiredAgentGrants(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM agent_grants WHERE expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("signup.DeleteExpiredAgentGrants: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func scanAgentGrant(row rowScanner) (*AgentGrant, error) {
	var g AgentGrant
	err := row.Scan(
		&g.ID, &g.DeviceCodeHash, &g.UserCode, &g.ClientName, &g.Platform,
		&g.IdentityID, &g.Status, &g.ExpiresAt, &g.DecidedAt, &g.RedeemedAt,
		&g.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation on the named constraint. Matching the constraint name keeps
// unrelated 23505s (e.g. device_code_hash) surfacing as real errors.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// isInvalidTextRepresentation reports whether err is PostgreSQL's 22P02, raised
// when a value cannot be cast to the target type — in practice a malformed UUID
// reaching a uuid column. Callers map it to "not found" so a bad identifier is a
// 4xx rather than a 500 with an error-level log line.
func isInvalidTextRepresentation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "22P02"
}
