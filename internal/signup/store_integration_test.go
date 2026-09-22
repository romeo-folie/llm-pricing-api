//go:build integration

package signup_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"llm-pricing-api/internal/signup"
)

// newTestPool connects to the test database specified by DATABASE_URL.
// Skips the test (rather than failing) when DATABASE_URL is not set or the
// database is unreachable, so integration tests are CI-safe when no DB is up.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("pgxpool.New: %v — skipping (database unavailable)", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v — skipping integration test", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// randEmail returns a unique email address for each test run.
func randEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
}

// randRawToken returns a unique raw token string for each test run, avoiding
// UNIQUE(token_hash) collisions when tests run against a persistent dev DB.
func randRawToken(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("integ-token-%d", time.Now().UnixNano())
}

// ── UpsertIdentity ────────────────────────────────────────────────────────────

func TestIntegration_UpsertIdentity_ReturnsExistingOnDuplicate(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)
	email := randEmail(t)

	id1, err := s.UpsertIdentity(ctx, email, "", "")
	if err != nil {
		t.Fatalf("first UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id1.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id1.ID, err)
		}
	})

	id2, err := s.UpsertIdentity(ctx, email, "", "")
	if err != nil {
		t.Fatalf("second UpsertIdentity (duplicate): %v", err)
	}
	if id1.ID != id2.ID {
		t.Errorf("duplicate email returned different IDs: %q vs %q", id1.ID, id2.ID)
	}
}

func TestIntegration_UpsertIdentity_NormalizesEmail(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	nano := time.Now().UnixNano()
	mixed := fmt.Sprintf("  MIXED-%d@Example.Com  ", nano)
	want := fmt.Sprintf("mixed-%d@example.com", nano)

	id, err := s.UpsertIdentity(ctx, mixed, "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})

	if id.Email != want {
		t.Errorf("email normalisation: got %q, want %q", id.Email, want)
	}
}

// ── ConsumeToken ──────────────────────────────────────────────────────────────

func TestIntegration_ConsumeToken_OneTimeUse(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})

	raw := randRawToken(t)
	tokenHash := signup.HashToken(raw)
	_, err = s.InsertToken(ctx, id.ID, tokenHash, time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("InsertToken: %v", err)
	}

	// First consume: should succeed.
	tok, err := s.ConsumeToken(ctx, tokenHash)
	if err != nil {
		t.Fatalf("first ConsumeToken: %v", err)
	}
	if tok.IdentityID != id.ID {
		t.Errorf("ConsumeToken returned identityID %q, want %q", tok.IdentityID, id.ID)
	}

	// Second consume: must return ErrTokenConsumed.
	_, err = s.ConsumeToken(ctx, tokenHash)
	if err == nil {
		t.Fatal("second ConsumeToken should have failed")
	}
	if !errors.Is(err, signup.ErrTokenConsumed) {
		t.Errorf("second ConsumeToken: got %v, want ErrTokenConsumed", err)
	}
}

func TestIntegration_ConsumeToken_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})

	raw := randRawToken(t)
	tokenHash := signup.HashToken(raw)
	// expires_at well in the past (-10s) to avoid false-pass due to DB/app clock skew.
	_, err = s.InsertToken(ctx, id.ID, tokenHash, time.Now().Add(-10*time.Second))
	if err != nil {
		t.Fatalf("InsertToken (expired): %v", err)
	}

	_, err = s.ConsumeToken(ctx, tokenHash)
	if err == nil {
		t.Fatal("ConsumeToken of expired token should have failed")
	}
	if !errors.Is(err, signup.ErrTokenExpired) {
		t.Errorf("got %v, want ErrTokenExpired", err)
	}
}

func TestIntegration_ConsumeToken_NotFound(t *testing.T) {
	ctx := context.Background()
	s := signup.NewStore(newTestPool(t))

	_, err := s.ConsumeToken(ctx, "nonexistent-token-hash-xyz")
	if !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// TestIntegration_InsertKey_AllowsMultipleUpToCap verifies the replacement for
// the old one-active-key-per-identity rule: keys are per-agent now, bounded by
// MaxActiveKeysPerIdentity rather than by a unique index.
func TestIntegration_InsertKey_AllowsMultipleUpToCap(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})

	nano := time.Now().UnixNano()
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		if _, err := s.InsertKeyWithLabel(ctx, id.ID, fmt.Sprintf("pkey-%d-%d", nano, i), "agent", signup.CreatedViaAgent); err != nil {
			t.Fatalf("InsertKeyWithLabel %d: %v", i, err)
		}
	}

	// One past the cap must be refused.
	_, err = s.InsertKey(ctx, id.ID, fmt.Sprintf("pkey-%d-over", nano))
	if !errors.Is(err, signup.ErrActiveKeyLimitReached) {
		t.Fatalf("insert past cap: err = %v, want ErrActiveKeyLimitReached", err)
	}

	keys, err := s.ListActiveKeys(ctx, id.ID)
	if err != nil {
		t.Fatalf("ListActiveKeys: %v", err)
	}
	if len(keys) != signup.MaxActiveKeysPerIdentity {
		t.Errorf("active keys = %d, want %d", len(keys), signup.MaxActiveKeysPerIdentity)
	}
}

// TestIntegration_InsertKey_ConcurrentInsertsRespectCap is the blocking test for
// the cap's race-freedom: without the identity row lock in InsertKeyWithLabel,
// concurrent inserts would all read an under-cap count and all succeed.
func TestIntegration_InsertKey_ConcurrentInsertsRespectCap(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})

	// Twice the cap, all racing.
	attempts := signup.MaxActiveKeysPerIdentity * 2
	nano := time.Now().UnixNano()
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		limited   int
		otherErr  []error
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.InsertKeyWithLabel(ctx, id.ID, fmt.Sprintf("pkey-race-%d-%d", nano, i), "racer", signup.CreatedViaAgent)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, signup.ErrActiveKeyLimitReached):
				limited++
			default:
				otherErr = append(otherErr, err)
			}
		}(i)
	}
	wg.Wait()

	for _, err := range otherErr {
		t.Errorf("unexpected error from concurrent insert: %v", err)
	}
	if succeeded != signup.MaxActiveKeysPerIdentity {
		t.Errorf("successful inserts = %d, want exactly %d", succeeded, signup.MaxActiveKeysPerIdentity)
	}
	if limited != attempts-signup.MaxActiveKeysPerIdentity {
		t.Errorf("limit rejections = %d, want %d", limited, attempts-signup.MaxActiveKeysPerIdentity)
	}

	keys, err := s.ListActiveKeys(ctx, id.ID)
	if err != nil {
		t.Fatalf("ListActiveKeys: %v", err)
	}
	if len(keys) != signup.MaxActiveKeysPerIdentity {
		t.Errorf("rows in registry = %d, want %d", len(keys), signup.MaxActiveKeysPerIdentity)
	}
}

func TestIntegration_RevokeKeyByID_ScopedToIdentity(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	owner, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity owner: %v", err)
	}
	other, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity other: %v", err)
	}
	t.Cleanup(func() {
		for _, id := range []string{owner.ID, other.ID} {
			if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id); err != nil {
				t.Logf("cleanup: failed to delete identity %s: %v", id, err)
			}
		}
	})

	nano := time.Now().UnixNano()
	pk := fmt.Sprintf("pkey-scope-%d", nano)
	k, err := s.InsertKeyWithLabel(ctx, owner.ID, pk, "Claude Code", signup.CreatedViaAgent)
	if err != nil {
		t.Fatalf("InsertKeyWithLabel: %v", err)
	}

	// Another identity must not be able to revoke it by ID.
	if _, _, err := s.RevokeKeyByID(ctx, other.ID, k.ID); !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("cross-identity revoke: err = %v, want ErrNotFound", err)
	}

	gotPK, alreadyRevoked, err := s.RevokeKeyByID(ctx, owner.ID, k.ID)
	if err != nil {
		t.Fatalf("RevokeKeyByID: %v", err)
	}
	if gotPK != pk {
		t.Errorf("returned provider key = %q, want %q", gotPK, pk)
	}
	if alreadyRevoked {
		t.Error("first revoke should not report alreadyRevoked")
	}

	// A repeat revoke succeeds and reports alreadyRevoked, which is what lets the
	// caller retry an upstream revocation that previously failed.
	gotPK, alreadyRevoked, err = s.RevokeKeyByID(ctx, owner.ID, k.ID)
	if err != nil {
		t.Fatalf("repeat revoke must be idempotent: %v", err)
	}
	if !alreadyRevoked {
		t.Error("repeat revoke should report alreadyRevoked")
	}
	if gotPK != pk {
		t.Errorf("repeat revoke returned provider key = %q, want %q", gotPK, pk)
	}

	// A malformed id reaches Postgres as a cast error, not a missing row. It must
	// surface as ErrNotFound so a bad identifier is a 4xx rather than a 500.
	if _, _, err := s.RevokeKeyByID(ctx, owner.ID, "not-a-uuid"); !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("malformed id: err = %v, want ErrNotFound", err)
	}
}

// ── Agent grants ──────────────────────────────────────────────────────────────

// newGrantFixture creates an identity and returns the store plus a cleanup func.
func newGrantFixture(t *testing.T) (signup.Store, *pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)
	id, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		// agent_grants cascades from api_identities, so one delete covers both.
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})
	return s, pool, id.ID
}

func TestIntegration_AgentGrant_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s, _, identityID := newGrantFixture(t)

	grant, rawDeviceCode, err := s.CreateAgentGrant(ctx, "Claude Code", "darwin", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateAgentGrant: %v", err)
	}
	if grant.Status != "pending" {
		t.Errorf("status = %q, want pending", grant.Status)
	}
	if grant.IdentityID != nil {
		t.Error("pending grant must not carry an identity_id")
	}
	// Only the hash may be persisted.
	if grant.DeviceCodeHash == rawDeviceCode {
		t.Error("raw device code was persisted")
	}
	if grant.DeviceCodeHash != signup.HashToken(rawDeviceCode) {
		t.Error("device_code_hash is not sha256(raw device code)")
	}
	if !signup.ValidUserCode(grant.UserCode) {
		t.Errorf("stored user code %q fails the format CHECK", grant.UserCode)
	}

	// The agent polls before approval.
	if _, err := s.RedeemAgentGrant(ctx, rawDeviceCode); !errors.Is(err, signup.ErrGrantPending) {
		t.Errorf("redeem before approval: err = %v, want ErrGrantPending", err)
	}

	// Lookup by raw device code and by user code both resolve the same grant.
	byDevice, err := s.GetAgentGrantByDeviceCode(ctx, rawDeviceCode)
	if err != nil {
		t.Fatalf("GetAgentGrantByDeviceCode: %v", err)
	}
	byCode, err := s.GetAgentGrantByUserCode(ctx, grant.UserCode)
	if err != nil {
		t.Fatalf("GetAgentGrantByUserCode: %v", err)
	}
	if byDevice.ID != byCode.ID {
		t.Errorf("lookups disagree: %s vs %s", byDevice.ID, byCode.ID)
	}

	// The user approves.
	decided, err := s.DecideAgentGrant(ctx, grant.UserCode, identityID, true)
	if err != nil {
		t.Fatalf("DecideAgentGrant: %v", err)
	}
	if decided.Status != "approved" {
		t.Fatalf("status = %q, want approved", decided.Status)
	}
	if decided.IdentityID == nil || *decided.IdentityID != identityID {
		t.Errorf("identity_id = %v, want %s", decided.IdentityID, identityID)
	}

	// Deciding twice is refused.
	if _, err := s.DecideAgentGrant(ctx, grant.UserCode, identityID, true); !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("second decide: err = %v, want ErrNotFound", err)
	}

	// The agent claims it — once.
	claimed, err := s.RedeemAgentGrant(ctx, rawDeviceCode)
	if err != nil {
		t.Fatalf("RedeemAgentGrant: %v", err)
	}
	if claimed.Status != "redeemed" || claimed.RedeemedAt == nil {
		t.Errorf("claimed grant = %+v, want status redeemed with redeemed_at", claimed)
	}
	if _, err := s.RedeemAgentGrant(ctx, rawDeviceCode); !errors.Is(err, signup.ErrGrantRedeemed) {
		t.Errorf("second redeem: err = %v, want ErrGrantRedeemed", err)
	}
}

func TestIntegration_AgentGrant_DenyLeavesNoIdentity(t *testing.T) {
	ctx := context.Background()
	s, _, identityID := newGrantFixture(t)

	grant, rawDeviceCode, err := s.CreateAgentGrant(ctx, "Claude Code", "", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateAgentGrant: %v", err)
	}

	denied, err := s.DecideAgentGrant(ctx, grant.UserCode, identityID, false)
	if err != nil {
		t.Fatalf("DecideAgentGrant(deny): %v", err)
	}
	if denied.Status != "denied" {
		t.Errorf("status = %q, want denied", denied.Status)
	}
	// A denial must not record which account was signed in.
	if denied.IdentityID != nil {
		t.Errorf("denied grant carries identity_id = %v, want nil", *denied.IdentityID)
	}

	if _, err := s.RedeemAgentGrant(ctx, rawDeviceCode); !errors.Is(err, signup.ErrGrantDenied) {
		t.Errorf("redeem denied grant: err = %v, want ErrGrantDenied", err)
	}
}

func TestIntegration_AgentGrant_ExpiredIsNotDecidableOrRedeemable(t *testing.T) {
	ctx := context.Background()
	s, _, identityID := newGrantFixture(t)

	grant, rawDeviceCode, err := s.CreateAgentGrant(ctx, "Claude Code", "", time.Now().Add(-time.Second))
	if err != nil {
		t.Fatalf("CreateAgentGrant: %v", err)
	}

	if _, err := s.DecideAgentGrant(ctx, grant.UserCode, identityID, true); !errors.Is(err, signup.ErrGrantExpired) {
		t.Errorf("decide expired: err = %v, want ErrGrantExpired", err)
	}
	if _, err := s.RedeemAgentGrant(ctx, rawDeviceCode); !errors.Is(err, signup.ErrGrantExpired) {
		t.Errorf("redeem expired: err = %v, want ErrGrantExpired", err)
	}
}

// RedeemAgentGrant is the claim that makes the plaintext single-use; concurrent
// polls must produce exactly one winner.
func TestIntegration_AgentGrant_ConcurrentRedeemIsExactlyOnce(t *testing.T) {
	ctx := context.Background()
	s, _, identityID := newGrantFixture(t)

	grant, rawDeviceCode, err := s.CreateAgentGrant(ctx, "Claude Code", "", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateAgentGrant: %v", err)
	}
	if _, err := s.DecideAgentGrant(ctx, grant.UserCode, identityID, true); err != nil {
		t.Fatalf("DecideAgentGrant: %v", err)
	}

	const pollers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		redeemed int
		refused  int
		otherErr []error
	)
	for i := 0; i < pollers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.RedeemAgentGrant(ctx, rawDeviceCode)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				redeemed++
			case errors.Is(err, signup.ErrGrantRedeemed):
				refused++
			default:
				otherErr = append(otherErr, err)
			}
		}()
	}
	wg.Wait()

	for _, err := range otherErr {
		t.Errorf("unexpected error from concurrent redeem: %v", err)
	}
	if redeemed != 1 {
		t.Errorf("successful redeems = %d, want exactly 1", redeemed)
	}
	if refused != pollers-1 {
		t.Errorf("ErrGrantRedeemed count = %d, want %d", refused, pollers-1)
	}
}

// A failed key issuance reverts the claim so the agent can retry rather than
// being stranded with a consumed grant.
func TestIntegration_AgentGrant_RevertRestoresApprovedState(t *testing.T) {
	ctx := context.Background()
	s, _, identityID := newGrantFixture(t)

	grant, rawDeviceCode, err := s.CreateAgentGrant(ctx, "Claude Code", "", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateAgentGrant: %v", err)
	}
	if _, err := s.DecideAgentGrant(ctx, grant.UserCode, identityID, true); err != nil {
		t.Fatalf("DecideAgentGrant: %v", err)
	}
	claimed, err := s.RedeemAgentGrant(ctx, rawDeviceCode)
	if err != nil {
		t.Fatalf("RedeemAgentGrant: %v", err)
	}

	if err := s.RevertAgentGrantRedemption(ctx, claimed.ID); err != nil {
		t.Fatalf("RevertAgentGrantRedemption: %v", err)
	}

	reverted, err := s.GetAgentGrantByDeviceCode(ctx, rawDeviceCode)
	if err != nil {
		t.Fatalf("GetAgentGrantByDeviceCode: %v", err)
	}
	if reverted.Status != "approved" {
		t.Errorf("status = %q, want approved", reverted.Status)
	}
	if reverted.RedeemedAt != nil {
		t.Error("redeemed_at was not cleared")
	}

	// Reverting a grant that is not currently redeemed is a no-op, so a second
	// revert while it sits in the approved state reports ErrNotFound. This is
	// checked before re-redeeming, since re-redeeming makes it revertible again.
	if err := s.RevertAgentGrantRedemption(ctx, claimed.ID); !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("revert non-redeemed: err = %v, want ErrNotFound", err)
	}

	// The revert re-armed the grant, so the agent's retry succeeds.
	reclaimed, err := s.RedeemAgentGrant(ctx, rawDeviceCode)
	if err != nil {
		t.Fatalf("redeem after revert: %v", err)
	}
	if reclaimed.Status != "redeemed" {
		t.Errorf("status after re-redeem = %q, want redeemed", reclaimed.Status)
	}
}

func TestIntegration_AgentGrant_UserCodesAreUniqueAndWellFormed(t *testing.T) {
	ctx := context.Background()
	s, pool, _ := newGrantFixture(t)

	seen := make(map[string]bool, 20)
	for i := 0; i < 20; i++ {
		g, _, err := s.CreateAgentGrant(ctx, "Claude Code", "", time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("CreateAgentGrant %d: %v", i, err)
		}
		if !signup.ValidUserCode(g.UserCode) {
			t.Fatalf("user code %q is not well formed", g.UserCode)
		}
		if seen[g.UserCode] {
			t.Fatalf("duplicate user code %q", g.UserCode)
		}
		seen[g.UserCode] = true
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM agent_grants WHERE client_name = $1`, "Claude Code"); err != nil {
			t.Logf("cleanup: failed to delete test grants: %v", err)
		}
	})
}

func TestIntegration_AgentGrant_DeleteExpired(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newGrantFixture(t)

	expired, _, err := s.CreateAgentGrant(ctx, "expired-agent", "", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("CreateAgentGrant (expired): %v", err)
	}
	live, _, err := s.CreateAgentGrant(ctx, "live-agent", "", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateAgentGrant (live): %v", err)
	}

	if _, err := s.DeleteExpiredAgentGrants(ctx); err != nil {
		t.Fatalf("DeleteExpiredAgentGrants: %v", err)
	}

	if _, err := s.GetAgentGrantByUserCode(ctx, expired.UserCode); !errors.Is(err, signup.ErrNotFound) {
		t.Errorf("expired grant still present: err = %v", err)
	}
	if _, err := s.GetAgentGrantByUserCode(ctx, live.UserCode); err != nil {
		t.Errorf("live grant was deleted: %v", err)
	}
}

func TestIntegration_RevokeKey_AllowsNewKeyAfterRevoke(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.UpsertIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID); err != nil {
			t.Logf("cleanup: failed to delete identity %s: %v", id.ID, err)
		}
	})

	pk1 := fmt.Sprintf("pkey-revoke-%d-1", time.Now().UnixNano())
	if _, err = s.InsertKey(ctx, id.ID, pk1); err != nil {
		t.Fatalf("InsertKey: %v", err)
	}
	if err = s.RevokeKey(ctx, id.ID, pk1); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}

	pk2 := fmt.Sprintf("pkey-revoke-%d-2", time.Now().UnixNano())
	if _, err = s.InsertKey(ctx, id.ID, pk2); err != nil {
		t.Fatalf("InsertKey after revoke: %v", err)
	}

	active, err := s.GetActiveKey(ctx, id.ID)
	if err != nil {
		t.Fatalf("GetActiveKey: %v", err)
	}
	if active.ProviderKeyID != pk2 {
		t.Errorf("active key is %q, want %q", active.ProviderKeyID, pk2)
	}
}

// TestIntegration_RevokeAndInsertKey_RespectsCap covers the cap bypass that the
// rotation path used to allow: it inserted without taking the identity lock or
// counting, so N concurrent rotations of one key left N extra active keys.
func TestIntegration_RevokeAndInsertKey_RespectsCap(t *testing.T) {
	ctx := context.Background()
	s, pool, identityID := newGrantFixture(t)

	nano := time.Now().UnixNano()
	// Fill to the cap.
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		if _, err := s.InsertKeyWithLabel(ctx, identityID, fmt.Sprintf("cap-%d-%d", nano, i), "agent", signup.CreatedViaAgent); err != nil {
			t.Fatalf("seed key %d: %v", i, err)
		}
	}

	// Rotating an existing key is net-neutral and must still succeed.
	keys, err := s.ListActiveKeys(ctx, identityID)
	if err != nil {
		t.Fatalf("ListActiveKeys: %v", err)
	}
	if _, err := s.RevokeAndInsertKey(ctx, identityID, keys[0].ProviderKeyID, fmt.Sprintf("rotated-%d", nano)); err != nil {
		t.Fatalf("rotation at the cap must succeed (net-neutral): %v", err)
	}

	// Inserting without revoking anything must be refused even though it comes
	// through the rotation entry point rather than InsertKeyWithLabel.
	_, err = s.RevokeAndInsertKey(ctx, identityID, "", fmt.Sprintf("uncapped-%d", nano))
	if !errors.Is(err, signup.ErrActiveKeyLimitReached) {
		t.Fatalf("pure insert at the cap: err = %v, want ErrActiveKeyLimitReached", err)
	}

	after, err := s.ListActiveKeys(ctx, identityID)
	if err != nil {
		t.Fatalf("ListActiveKeys: %v", err)
	}
	if len(after) != signup.MaxActiveKeysPerIdentity {
		t.Errorf("active keys = %d, want %d", len(after), signup.MaxActiveKeysPerIdentity)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, identityID); err != nil {
		t.Logf("cleanup: %v", err)
	}
}

// TestIntegration_RevokeAndInsertKey_ConcurrentRotationsRespectCap is the
// regression test for the concrete interleaving: two rotations of the SAME key
// both revoke nothing (the second UPDATE matches zero rows, which is
// deliberately non-fatal) and both insert, leaving one extra key per racing pair.
func TestIntegration_RevokeAndInsertKey_ConcurrentRotationsRespectCap(t *testing.T) {
	ctx := context.Background()
	s, pool, identityID := newGrantFixture(t)

	nano := time.Now().UnixNano()
	for i := 0; i < signup.MaxActiveKeysPerIdentity; i++ {
		if _, err := s.InsertKeyWithLabel(ctx, identityID, fmt.Sprintf("race-%d-%d", nano, i), "agent", signup.CreatedViaAgent); err != nil {
			t.Fatalf("seed key %d: %v", i, err)
		}
	}
	keys, err := s.ListActiveKeys(ctx, identityID)
	if err != nil {
		t.Fatalf("ListActiveKeys: %v", err)
	}
	target := keys[0].ProviderKeyID

	// Every racer rotates the same target.
	const racers = 6
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		failed  int
		otherEr []error
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.RevokeAndInsertKey(ctx, identityID, target, fmt.Sprintf("race-new-%d-%d", nano, i))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
			case errors.Is(err, signup.ErrActiveKeyLimitReached):
				failed++
			default:
				otherEr = append(otherEr, err)
			}
		}(i)
	}
	wg.Wait()

	for _, err := range otherEr {
		t.Errorf("unexpected error: %v", err)
	}

	after, err := s.ListActiveKeys(ctx, identityID)
	if err != nil {
		t.Fatalf("ListActiveKeys: %v", err)
	}
	if len(after) > signup.MaxActiveKeysPerIdentity {
		t.Errorf("active keys = %d after %d concurrent rotations, cap is %d — cap was bypassed",
			len(after), racers, signup.MaxActiveKeysPerIdentity)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, identityID); err != nil {
		t.Logf("cleanup: %v", err)
	}
}
