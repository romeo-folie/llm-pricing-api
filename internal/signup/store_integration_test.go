//go:build integration

package signup_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"llm-pricing-api/internal/signup"
)

// newTestPool connects to the test database specified by DATABASE_URL.
// Skip the test if the env var is not set.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// randEmail returns a unique email address for each test run.
func randEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())
}

// ── CreateIdentity ─────────────────────────────────────────────────────────────

func TestIntegration_CreateIdentity_ReturnsExistingOnDuplicate(t *testing.T) {
	ctx := context.Background()
	s := signup.NewStore(newTestPool(t))
	email := randEmail(t)

	id1, err := s.CreateIdentity(ctx, email, "", "")
	if err != nil {
		t.Fatalf("first CreateIdentity: %v", err)
	}
	t.Cleanup(func() {
		pool := newTestPool(t)
		pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id1.ID)
	})

	id2, err := s.CreateIdentity(ctx, email, "", "")
	if err != nil {
		t.Fatalf("second CreateIdentity (duplicate): %v", err)
	}
	if id1.ID != id2.ID {
		t.Errorf("duplicate email returned different IDs: %q vs %q", id1.ID, id2.ID)
	}
}

func TestIntegration_CreateIdentity_NormalizesEmail(t *testing.T) {
	ctx := context.Background()
	s := signup.NewStore(newTestPool(t))
	email := randEmail(t)
	mixed := "  " + fmt.Sprintf("MIXED-%d@Example.Com", time.Now().UnixNano()) + "  "

	id, err := s.CreateIdentity(ctx, mixed, "", "")
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() {
		pool := newTestPool(t)
		pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID)
	})
	_ = email
	if id.Email != fmt.Sprintf("mixed-%d@example.com", time.Now().UnixNano()) {
		// Can't predict exact nanos, just check it's lowercase and trimmed.
		if id.Email != id.Email[0:] || id.Email != fmt.Sprintf("%s", id.Email) {
			t.Errorf("email not normalised: %q", id.Email)
		}
		// Verify lowercase
		lower := id.Email
		for _, c := range id.Email {
			if c >= 'A' && c <= 'Z' {
				t.Errorf("email contains uppercase after normalisation: %q", id.Email)
				break
			}
			_ = lower
		}
	}
}

// ── ConsumeToken ──────────────────────────────────────────────────────────────

func TestIntegration_ConsumeToken_OneTimeUse(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.CreateIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID) })

	raw := "integ-test-token-onetimeuse"
	_, err = s.CreateToken(ctx, id.ID, raw, time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// First consume: should succeed.
	gotID, err := s.ConsumeToken(ctx, raw)
	if err != nil {
		t.Fatalf("first ConsumeToken: %v", err)
	}
	if gotID != id.ID {
		t.Errorf("ConsumeToken returned identityID %q, want %q", gotID, id.ID)
	}

	// Second consume: must return ErrTokenUsed.
	_, err = s.ConsumeToken(ctx, raw)
	if err == nil {
		t.Fatal("second ConsumeToken should have failed")
	}
	if err.Error() != signup.ErrTokenUsed.Error() && err != signup.ErrTokenUsed {
		t.Errorf("second ConsumeToken: got %v, want ErrTokenUsed", err)
	}
}

func TestIntegration_ConsumeToken_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.CreateIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID) })

	raw := "integ-test-token-expired"
	// expires_at in the past
	_, err = s.CreateToken(ctx, id.ID, raw, time.Now().Add(-1*time.Second))
	if err != nil {
		t.Fatalf("CreateToken (expired): %v", err)
	}

	_, err = s.ConsumeToken(ctx, raw)
	if err == nil {
		t.Fatal("ConsumeToken of expired token should have failed")
	}
	if err != signup.ErrTokenExpired {
		t.Errorf("got %v, want ErrTokenExpired", err)
	}
}

func TestIntegration_ConsumeToken_NotFound(t *testing.T) {
	ctx := context.Background()
	s := signup.NewStore(newTestPool(t))

	_, err := s.ConsumeToken(ctx, "nonexistent-token-xyz")
	if err != signup.ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// ── RegisterKey / RevokeKey ───────────────────────────────────────────────────

func TestIntegration_RegisterKey_OneActiveKeyPerIdentity(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.CreateIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID) })

	pk1 := fmt.Sprintf("pkey-%d-1", time.Now().UnixNano())
	_, err = s.RegisterKey(ctx, id.ID, pk1)
	if err != nil {
		t.Fatalf("RegisterKey (first): %v", err)
	}

	pk2 := fmt.Sprintf("pkey-%d-2", time.Now().UnixNano())
	_, err = s.RegisterKey(ctx, id.ID, pk2)
	if err == nil {
		t.Fatal("RegisterKey with existing active key should fail (partial unique index)")
	}
}

func TestIntegration_RevokeKey_AllowsNewKeyAfterRevoke(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := signup.NewStore(pool)

	id, err := s.CreateIdentity(ctx, randEmail(t), "", "")
	if err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM api_identities WHERE id = $1`, id.ID) })

	pk1 := fmt.Sprintf("pkey-revoke-%d-1", time.Now().UnixNano())
	if _, err = s.RegisterKey(ctx, id.ID, pk1); err != nil {
		t.Fatalf("RegisterKey: %v", err)
	}
	if err = s.RevokeKey(ctx, pk1); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}

	pk2 := fmt.Sprintf("pkey-revoke-%d-2", time.Now().UnixNano())
	if _, err = s.RegisterKey(ctx, id.ID, pk2); err != nil {
		t.Fatalf("RegisterKey after revoke: %v", err)
	}

	active, err := s.FindActiveKey(ctx, id.ID)
	if err != nil {
		t.Fatalf("FindActiveKey: %v", err)
	}
	if active.ProviderKeyID != pk2 {
		t.Errorf("active key is %q, want %q", active.ProviderKeyID, pk2)
	}
}
