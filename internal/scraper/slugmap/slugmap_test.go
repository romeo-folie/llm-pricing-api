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
		{"claude-opus-4-6", "anthropic/claude-4-6-opus"},
		{"claude-sonnet-4-6", "anthropic/claude-4-6-sonnet"},
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

func TestResolve_CaseInsensitive(t *testing.T) {
	got, ok := Resolve("  GPT-4O-MINI  ")
	if !ok || got != "openai/gpt-4o-mini" {
		t.Errorf("Resolve with whitespace/uppercase = (%q, %v); want (openai/gpt-4o-mini, true)", got, ok)
	}
}
