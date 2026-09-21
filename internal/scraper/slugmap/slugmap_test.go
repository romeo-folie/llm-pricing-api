package slugmap

import "testing"

func TestResolve_ExactMatch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpt-4o", "openai/gpt-4o"},
		{"GPT-4o", "openai/gpt-4o"},
		{"gpt-4o-mini", "openai/gpt-4o-mini"},
		{"claude-3-5-sonnet-20241022", "anthropic/claude-3-5-sonnet"},
		{"claude-3.5-sonnet", "anthropic/claude-3-5-sonnet"},
		{"claude-opus-4-6", "anthropic/claude-opus-4.6"},
		{"claude-sonnet-4-6", "anthropic/claude-sonnet-4.6"},
		{"claude-sonnet-4-20250514", "anthropic/claude-sonnet-4"},
		{"claude-opus-4-20250514", "anthropic/claude-opus-4"},
		{"gemini-2.0-flash", "google/gemini-2.0-flash"},
		{"gemini-2.5-pro", "google/gemini-2.5-pro"},
		{"llama-3.3-70b-instruct", "meta-llama/llama-3.3-70b-instruct"},
		{"mistral-large", "mistralai/mistral-large"},
		{"deepseek-v3", "deepseek/deepseek-chat"},
		{"deepseek-r1", "deepseek/deepseek-reasoner"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := Resolve(tc.input)
			if !ok {
				t.Fatalf("Resolve(%q) returned ok=false; want %q", tc.input, tc.want)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolve_PrefixFallback(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-3-5-sonnet-20250101", "anthropic/claude-3-5-sonnet"},
		{"gpt-4o-2025-01-01", "openai/gpt-4o"},
		{"gemini-2.5-pro-exp-0827", "google/gemini-2.5-pro"},
		{"llama-3.3-70b-instruct-turbo", "meta-llama/llama-3.3-70b-instruct"},
		{"mistral-large-2501", "mistralai/mistral-large"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := Resolve(tc.input)
			if !ok {
				t.Fatalf("Resolve(%q) returned ok=false; want %q", tc.input, tc.want)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolve_NoMatch(t *testing.T) {
	unknowns := []string{
		"some-unknown-model",
		"",
		"random-text-123",
		"claude-sonnet-4-5unknown",
		"gpt-4oish",
	}
	for _, input := range unknowns {
		t.Run(input, func(t *testing.T) {
			_, ok := Resolve(input)
			if ok {
				t.Errorf("Resolve(%q) returned ok=true; want false", input)
			}
		})
	}
}

func TestResolve_DoesNotCrossClaudeGenerations(t *testing.T) {
	unknowns := []string{
		"claude-4-opus",
		"claude-opus-4",
		"claude-opus-4-20250514_nothink",
		"claude-4-sonnet",
		"claude-4-sonnet-20250522",
		"claude-sonnet-4",
		"claude-sonnet-4-20250514_nothink",
	}
	for _, input := range unknowns {
		t.Run(input, func(t *testing.T) {
			if slug, ok := Resolve(input); ok {
				t.Fatalf("Resolve(%q) = %q; cross-generation attribution must be rejected", input, slug)
			}
		})
	}

	controls := map[string]string{
		"claude-4-opus-20250514":   "anthropic/claude-opus-4",
		"claude-opus-4-20250514":   "anthropic/claude-opus-4",
		"claude-sonnet-4-20250514": "anthropic/claude-sonnet-4",
		"claude-sonnet-4-5":        "anthropic/claude-sonnet-4.5",
		"claude-opus-4-5":          "anthropic/claude-opus-4.5",
		"claude-sonnet-4-6":        "anthropic/claude-sonnet-4.6",
		"claude-opus-4-6":          "anthropic/claude-opus-4.6",
	}
	for input, want := range controls {
		if got, ok := Resolve(input); !ok || got != want {
			t.Errorf("Resolve(%q) = (%q, %v); want (%q, true)", input, got, ok, want)
		}
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	got, ok := Resolve("  GPT-4O-MINI  ")
	if !ok || got != "openai/gpt-4o-mini" {
		t.Errorf("Resolve with whitespace/uppercase = (%q, %v); want (openai/gpt-4o-mini, true)", got, ok)
	}
}

func TestResolveOutcome_Resolved(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpt-4o", "openai/gpt-4o"},            // exact
		{"GPT-4o", "openai/gpt-4o"},            // exact, case-folded
		{"gpt-4o-2025-01-01", "openai/gpt-4o"}, // single matching prefix rule
		{"claude-3-5-sonnet-20250101", "anthropic/claude-3-5-sonnet"},
		{"gemini-2.5-pro-exp-0827", "google/gemini-2.5-pro"},
		{"mistral-large-2501", "mistralai/mistral-large"},
		{"  claude-opus-4-6  ", "anthropic/claude-opus-4.6"}, // exact after trim
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, outcome := ResolveOutcome(tc.input)
			if outcome != OutcomeResolved {
				t.Fatalf("ResolveOutcome(%q) outcome = %q; want %q", tc.input, outcome, OutcomeResolved)
			}
			if got != tc.want {
				t.Errorf("ResolveOutcome(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveOutcome_Unknown(t *testing.T) {
	unknowns := []string{
		"some-unknown-model",
		"",
		"   ",
		"random-text-123",
		"gpt-4oish", // no delimiter after the known prefix
		"claude-sonnet-4-5unknown",
	}
	for _, input := range unknowns {
		t.Run(input, func(t *testing.T) {
			slug, outcome := ResolveOutcome(input)
			if outcome != OutcomeUnknown {
				t.Fatalf("ResolveOutcome(%q) outcome = %q; want %q", input, outcome, OutcomeUnknown)
			}
			if slug != "" {
				t.Errorf("ResolveOutcome(%q) slug = %q; want empty", input, slug)
			}
		})
	}
}

// TestResolveOutcome_Ambiguous covers the case the boolean Resolve cannot
// express: two allowlist prefix rules match the same name with different
// canonical slugs, so the winner is decided only by the order of
// prefixFallbacks. The greedy first match is still returned so existing callers
// keep resolving these names.
func TestResolveOutcome_Ambiguous(t *testing.T) {
	tests := []struct {
		input    string
		greedy   string
		conflict string
	}{
		// Matches both "gpt-4o-mini" and "gpt-4o".
		{"gpt-4o-mini-20250101", "openai/gpt-4o-mini", "gpt-4o"},
		// Matches both "gpt-4.1-mini" and "gpt-4.1".
		{"gpt-4.1-mini-20250101", "openai/gpt-4.1-mini", "gpt-4.1"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			// Guard the fixture: the second rule must genuinely match the
			// input, otherwise this test would silently stop covering
			// ambiguity if a prefix were renamed.
			if !hasKnownVariantPrefix(tc.input, tc.conflict) {
				t.Fatalf("test fixture is stale: %q does not match conflicting prefix %q", tc.input, tc.conflict)
			}

			slug, outcome := ResolveOutcome(tc.input)
			if outcome != OutcomeAmbiguous {
				t.Fatalf("ResolveOutcome(%q) outcome = %q; want %q", tc.input, outcome, OutcomeAmbiguous)
			}
			if slug != tc.greedy {
				t.Errorf("ResolveOutcome(%q) slug = %q; want greedy first match %q", tc.input, slug, tc.greedy)
			}
		})
	}
}

// TestResolve_AmbiguousStaysBackwardsCompatible pins the compatibility promise
// of the thin wrapper: a name that Resolve accepted before this variant existed
// must still resolve with ok=true and the same slug.
func TestResolve_AmbiguousStaysBackwardsCompatible(t *testing.T) {
	slug, ok := Resolve("gpt-4o-mini-20250101")
	if !ok {
		t.Fatal("Resolve() returned ok=false for an ambiguous name; existing callers must be unaffected")
	}
	if slug != "openai/gpt-4o-mini" {
		t.Errorf("Resolve() = %q; want openai/gpt-4o-mini", slug)
	}
}

func TestOutcome_String(t *testing.T) {
	for _, tc := range []struct {
		outcome Outcome
		want    string
	}{
		{OutcomeResolved, "resolved"},
		{OutcomeUnknown, "unknown"},
		{OutcomeAmbiguous, "ambiguous"},
	} {
		if got := tc.outcome.String(); got != tc.want {
			t.Errorf("Outcome(%q).String() = %q; want %q", string(tc.outcome), got, tc.want)
		}
	}
}
