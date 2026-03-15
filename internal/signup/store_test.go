package signup_test

import (
	"errors"
	"testing"

	"llm-pricing-api/internal/signup"
)

// ── HashToken ─────────────────────────────────────────────────────────────────

func TestHashToken_Deterministic(t *testing.T) {
	raw := "test-token-abc123"
	h1 := signup.HashToken(raw)
	h2 := signup.HashToken(raw)
	if h1 != h2 {
		t.Errorf("HashToken is not deterministic: %q vs %q", h1, h2)
	}
}

func TestHashToken_DifferentInputsDifferentOutputs(t *testing.T) {
	h1 := signup.HashToken("token-a")
	h2 := signup.HashToken("token-b")
	if h1 == h2 {
		t.Error("HashToken collision: different inputs produced same hash")
	}
}

func TestHashToken_EmptyString(t *testing.T) {
	h := signup.HashToken("")
	if h == "" {
		t.Error("HashToken of empty string should not be empty")
	}
}

// ── Sentinel error tests ──────────────────────────────────────────────────────
// These tests verify the exported error sentinels are distinct from one another.
// Full DB-backed tests are integration tests gated by a DATABASE_URL env var.

func TestSentinels_AreDistinct(t *testing.T) {
	if errors.Is(signup.ErrNotFound, signup.ErrTokenUsed) {
		t.Error("ErrNotFound and ErrTokenUsed must be distinct")
	}
	if errors.Is(signup.ErrNotFound, signup.ErrTokenExpired) {
		t.Error("ErrNotFound and ErrTokenExpired must be distinct")
	}
	if errors.Is(signup.ErrTokenUsed, signup.ErrTokenExpired) {
		t.Error("ErrTokenUsed and ErrTokenExpired must be distinct")
	}
}

// ── HashToken fixed-vector tests ──────────────────────────────────────────────
// ConsumeToken's one-time-use and expiry semantics are covered by integration
// tests (TestIntegration_ConsumeToken_*) that require a live DB. Unit coverage
// of ConsumeToken belongs there; the following tests verify HashToken output
// format and known SHA-256 vectors.

func TestHashToken_FixedVector(t *testing.T) {
	// SHA-256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	// SHA-256("abc") = ba7816bf8f01cfea414140de5dae2ec73b00361bbef0469fa72a67b86e22ce15...
	// Each test case checks the first 8 hex characters of the expected digest.
	cases := []struct {
		raw    string
		prefix string // first 8 hex chars of expected SHA-256
	}{
		{"", "e3b0c442"},
		{"abc", "ba7816bf"},
	}
	for _, tc := range cases {
		got := signup.HashToken(tc.raw)
		if len(got) < 8 || got[:8] != tc.prefix {
			t.Errorf("HashToken(%q): got %q, want prefix %q", tc.raw, got, tc.prefix)
		}
	}
}

// ── KeyRecord / Token zero-value sanity ────────────────────────────────────────

func TestKeyRecord_ZeroValue(t *testing.T) {
	var k signup.KeyRecord
	if k.Status != "" {
		t.Error("zero-value KeyRecord should have empty Status")
	}
	if k.RevokedAt != nil {
		t.Error("zero-value KeyRecord.RevokedAt should be nil")
	}
}

func TestToken_ZeroValue(t *testing.T) {
	var tok signup.Token
	if tok.UsedAt != nil {
		t.Error("zero-value Token.UsedAt should be nil (unused)")
	}
	if !tok.ExpiresAt.IsZero() {
		t.Error("zero-value Token.ExpiresAt should be zero time")
	}
}


