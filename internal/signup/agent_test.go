package signup_test

import (
	"strings"
	"testing"

	"llm-pricing-api/internal/signup"
)

// ── User codes ────────────────────────────────────────────────────────────────

func TestNewUserCode_Format(t *testing.T) {
	seen := make(map[string]bool, 500)
	for i := 0; i < 500; i++ {
		code, err := signup.NewUserCode()
		if err != nil {
			t.Fatalf("NewUserCode: %v", err)
		}
		if len(code) != signup.UserCodeLength {
			t.Fatalf("code %q has length %d, want %d", code, len(code), signup.UserCodeLength)
		}
		if !signup.ValidUserCode(code) {
			t.Fatalf("code %q fails ValidUserCode — it would violate the DB CHECK constraint", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %q generated within 500 draws", code)
		}
		seen[code] = true
	}
}

// The alphabet omits I, L, O and U precisely so a code cannot be misread; that
// is only true if generation never emits them.
func TestNewUserCode_ExcludesAmbiguousCharacters(t *testing.T) {
	for i := 0; i < 500; i++ {
		code, err := signup.NewUserCode()
		if err != nil {
			t.Fatalf("NewUserCode: %v", err)
		}
		if strings.ContainsAny(code, "ILOU") {
			t.Fatalf("code %q contains an ambiguous character", code)
		}
	}
}

func TestNormalizeUserCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"already canonical", "ACDF2345", "ACDF2345"},
		{"display hyphen removed", "ACDF-2345", "ACDF2345"},
		{"lower case upper-cased", "acdf2345", "ACDF2345"},
		{"surrounding whitespace", "  ACDF2345  ", "ACDF2345"},
		{"internal spaces removed", "ACDF 2345", "ACDF2345"},
		{"underscores removed", "ACDF_2345", "ACDF2345"},
		// Crockford aliases: I/L read as 1, O as 0.
		{"I folds to 1", "ACDF-I345", "ACDF1345"},
		{"L folds to 1", "ACDF-L345", "ACDF1345"},
		{"O folds to 0", "ACDF-O345", "ACDF0345"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := signup.NormalizeUserCode(tc.in); got != tc.want {
				t.Errorf("NormalizeUserCode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidUserCode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ACDF2345", true},
		{"00000000", true},
		{"ZZZZZZZZ", true},
		{"ACDF234", false},   // too short
		{"ACDF23456", false}, // too long
		{"ACDF-I345", false}, // hyphen must be normalised away first
		{"acdf2345", false},  // lower case must be normalised first
		{"ACDFI345", false},  // I excluded
		{"ACDFL345", false},  // L excluded
		{"ACDFO345", false},  // O excluded
		{"ACDFU345", false},  // U excluded
		{"ACDF-345", false},  // wrong length after hyphen
		{"", false},
	}
	for _, tc := range cases {
		if got := signup.ValidUserCode(tc.in); got != tc.want {
			t.Errorf("ValidUserCode(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFormatUserCode(t *testing.T) {
	if got := signup.FormatUserCode("ACDF2345"); got != "ACDF-2345" {
		t.Errorf("FormatUserCode = %q, want ACDF-2345", got)
	}
	// Anything unexpected is passed through unchanged rather than panicking.
	if got := signup.FormatUserCode("SHORT"); got != "SHORT" {
		t.Errorf("FormatUserCode(SHORT) = %q, want unchanged", got)
	}
}

// A formatted code must round-trip: display it, and normalising it back yields
// the stored form. That is what makes the hyphenated code safe to read aloud.
func TestUserCode_DisplayRoundTrip(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, err := signup.NewUserCode()
		if err != nil {
			t.Fatalf("NewUserCode: %v", err)
		}
		if got := signup.NormalizeUserCode(signup.FormatUserCode(code)); got != code {
			t.Fatalf("round trip of %q produced %q", code, got)
		}
	}
}

// ── Device codes ──────────────────────────────────────────────────────────────

func TestNewDeviceCode_HighEntropyAndUnique(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		code, err := signup.NewDeviceCode()
		if err != nil {
			t.Fatalf("NewDeviceCode: %v", err)
		}
		if code == "" {
			t.Fatal("empty device code")
		}
		// base64url with no padding: safe in JSON, headers and query strings.
		if strings.ContainsAny(code, "+/=") {
			t.Fatalf("device code %q is not URL-safe", code)
		}
		if seen[code] {
			t.Fatalf("duplicate device code %q", code)
		}
		seen[code] = true
	}
}

// ── Agent-supplied field validation ───────────────────────────────────────────

func TestValidateAgentClientName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"normal", "Claude Code", false},
		{"unicode is fine", "Cursor（テスト）", false},
		{"at the byte limit", strings.Repeat("a", 64), false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"one byte over", strings.Repeat("a", 65), true},
		// Control characters are an injection vector in a browser and in a
		// terminal, both of which echo this value.
		{"escape sequence", "evil\x1b[31mred", true},
		{"newline", "line\nbreak", true},
		{"tab", "tab\there", true},
		{"bell", "ding\x07", true},
		{"embedded carriage return", "line1\rline2", true},
		// Category Cf is not "control" per unicode.IsControl, but these are the
		// characters that can make a consent screen read differently than it is
		// (bidi overrides) or make two agent names look identical (zero-width).
		{"right-to-left override", "Code\u202eevil", true},
		{"left-to-right override", "\u202dCode", true},
		{"zero-width space", "Claude\u200bCode", true},
		{"zero-width joiner", "Claude\u200dCode", true},
		{"BOM / zero-width no-break", "Claude\ufeffCode", true},
		{"line separator", "line\u2028break", true},
		{"paragraph separator", "para\u2029break", true},
		// Surrounding whitespace is trimmed rather than rejected, so a trailing
		// CR/LF cannot reach the stored value either.
		{"trailing newline is trimmed", "ok\n", false},
		{"leading tab is trimmed", "\tok", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := signup.ValidateAgentClientName(tc.in)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateAgentClientName(%q) = nil, want error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateAgentClientName(%q) = %v, want nil", tc.in, err)
			}
		})
	}
}

func TestValidateAgentPlatform(t *testing.T) {
	if err := signup.ValidateAgentPlatform(""); err != nil {
		t.Errorf("empty platform should be allowed, got %v", err)
	}
	if err := signup.ValidateAgentPlatform("darwin-arm64"); err != nil {
		t.Errorf("normal platform rejected: %v", err)
	}
	if err := signup.ValidateAgentPlatform(strings.Repeat("a", 33)); err == nil {
		t.Error("overlong platform should be rejected")
	}
	if err := signup.ValidateAgentPlatform("esc\x1b[0m"); err == nil {
		t.Error("control characters in platform should be rejected")
	}
}

// ── SafeNextPath (open-redirect guard) ────────────────────────────────────────

func TestSafeNextPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain path", "/activate", "/activate"},
		{"path with unrelated query", "/compare?models=a,b", "/compare?models=a,b"},
		{"path with fragment", "/signup/verified#key", "/signup/verified#key"},
		{"surrounding whitespace", "  /activate  ", "/activate"},
		{"empty", "", ""},
		// A device-approval code must never survive the trip through an emailed
		// link. request-link is unauthenticated and mails an arbitrary address,
		// so a code in `next` would let an attacker send a victim a genuine
		// llmrates.live email that opens the approval screen for the attacker's
		// grant — defeating the code comparison the screen exists to provide.
		{"approval code is stripped", "/activate?code=ACDF-2345", "/activate"},
		{"approval code stripped, other params kept", "/activate?code=ACDF-2345&x=1", "/activate?x=1"},
		{"lower-case code param also stripped", "/activate?CODE=ACDF-2345", "/activate"},
		{"multiple code params stripped", "/activate?code=A&code=B", "/activate"},
		// Absolute URLs would bounce a user off-site from a trusted link.
		{"absolute http", "http://evil.example", ""},
		{"absolute https", "https://evil.example", ""},
		{"scheme-relative", "//evil.example", ""},
		{"backslash scheme-relative", "/\\evil.example", ""},
		{"backslash anywhere", "/act\\ivate", ""},
		{"relative, no leading slash", "activate", ""},
		{"protocol smuggling", "https:/evil.example", ""},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"data scheme", "data:text/html,<script>", ""},
		{"crlf header injection", "/activate\r\nLocation: https://evil.example", ""},
		{"overlong", "/" + strings.Repeat("a", 512), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := signup.SafeNextPath(tc.in); got != tc.want {
				t.Errorf("SafeNextPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// BuildVerifyURLWithNext must carry a safe next target through the emailed link,
// and must refuse to embed either an unsafe one or a device-approval code.
func TestBuildVerifyURLWithNext(t *testing.T) {
	got := signup.BuildVerifyURLWithNext("https://example.com", "/signup/verify", "tok123", "/activate")
	if !strings.Contains(got, "token=tok123") {
		t.Errorf("verify URL missing token: %q", got)
	}
	if !strings.Contains(got, "next=%2Factivate") {
		t.Errorf("verify URL missing encoded next: %q", got)
	}

	// A code in next is dropped before the link is emailed, so the emailed URL
	// cannot pre-load someone else's approval screen.
	got = signup.BuildVerifyURLWithNext("https://example.com", "/signup/verify", "tok123", "/activate?code=ACDF-2345")
	if strings.Contains(got, "code") {
		t.Errorf("approval code was embedded in the verify URL: %q", got)
	}

	// An unsafe target is dropped entirely rather than emailed.
	got = signup.BuildVerifyURLWithNext("https://example.com", "/signup/verify", "tok123", "https://evil.example")
	if strings.Contains(got, "next=") {
		t.Errorf("unsafe next was embedded in the verify URL: %q", got)
	}

	// BuildVerifyURL keeps its original behaviour: no next parameter at all.
	plain := signup.BuildVerifyURL("https://example.com", "/signup/verify", "tok123")
	if strings.Contains(plain, "next=") {
		t.Errorf("BuildVerifyURL should not add a next parameter: %q", plain)
	}
}
