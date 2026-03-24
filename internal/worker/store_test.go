package worker

import "testing"

func TestNameFromSlug(t *testing.T) {
	tests := []struct {
		slug string
		want string
	}{
		// Simple "provider/model" slug.
		{"openai/gpt-4", "gpt-4"},
		{"anthropic/claude-3-5-sonnet-20241022", "claude-3-5-sonnet-20241022"},
		// Deep Fireworks-style path slug.
		{"fireworks_ai/accounts/fireworks/models/stable-diffusion-xl-1024-v1-0", "stable-diffusion-xl-1024-v1-0"},
		{"fireworks_ai/accounts/fireworks/models/japanese-stable-diffusion-xl", "japanese-stable-diffusion-xl"},
		// Slug with no slash — returned as-is.
		{"gpt-4", "gpt-4"},
		// Trailing slash — returned as-is (degenerate input).
		{"openai/", "openai/"},
	}
	for _, tc := range tests {
		got := nameFromSlug(tc.slug)
		if got != tc.want {
			t.Errorf("nameFromSlug(%q) = %q; want %q", tc.slug, got, tc.want)
		}
	}
}
