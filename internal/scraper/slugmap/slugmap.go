// Package slugmap provides canonical slug resolution for model names
// as returned by external benchmark leaderboards.
package slugmap

import "strings"

// canonicalMap maps lowercased leaderboard model names (or partial names)
// to their canonical DB slugs. Exact matches are tried first; if none match,
// a contains-based fallback is used.
var canonicalMap = map[string]string{
	// OpenAI
	"gpt-4o":                        "openai/gpt-4o",
	"gpt-4o-2024-05-13":             "openai/gpt-4o",
	"gpt-4o-2024-08-06":             "openai/gpt-4o",
	"gpt-4o-2024-11-20":             "openai/gpt-4o",
	"gpt-4o-mini":                    "openai/gpt-4o-mini",
	"gpt-4o-mini-2024-07-18":        "openai/gpt-4o-mini",
	"gpt-4-turbo":                    "openai/gpt-4-turbo",
	"gpt-4-turbo-2024-04-09":        "openai/gpt-4-turbo",
	"gpt-4-1":                        "openai/gpt-4.1",
	"gpt-4.1":                        "openai/gpt-4.1",
	"gpt-4.1-mini":                   "openai/gpt-4.1-mini",
	"gpt-4.1-nano":                   "openai/gpt-4.1-nano",
	"o1":                             "openai/o1",
	"o1-preview":                     "openai/o1-preview",
	"o1-mini":                        "openai/o1-mini",
	"o3":                             "openai/o3",
	"o3-mini":                        "openai/o3-mini",
	"o4-mini":                        "openai/o4-mini",

	// Anthropic
	"claude-3-5-sonnet":              "anthropic/claude-3.5-sonnet",
	"claude-3-5-sonnet-20241022":     "anthropic/claude-3.5-sonnet",
	"claude-3-5-sonnet-20240620":     "anthropic/claude-3.5-sonnet",
	"claude-3.5-sonnet":              "anthropic/claude-3.5-sonnet",
	"claude-3-5-haiku":               "anthropic/claude-3.5-haiku",
	"claude-3-5-haiku-20241022":      "anthropic/claude-3.5-haiku",
	"claude-3.5-haiku":               "anthropic/claude-3.5-haiku",
	"claude-opus-4-6":                "anthropic/claude-opus-4-6",
	"claude-4-opus":                  "anthropic/claude-opus-4-6",
	"claude-sonnet-4-6":              "anthropic/claude-sonnet-4-6",
	"claude-4-sonnet":                "anthropic/claude-sonnet-4-6",
	"claude-sonnet-4":                "anthropic/claude-sonnet-4-6",
	"claude-3-opus":                  "anthropic/claude-3-opus",
	"claude-3-opus-20240229":         "anthropic/claude-3-opus",
	"claude-3-haiku":                 "anthropic/claude-3-haiku",
	"claude-3-haiku-20240307":        "anthropic/claude-3-haiku",

	// Google
	"gemini-2.0-flash":               "google/gemini-2.0-flash",
	"gemini-2.0-flash-001":           "google/gemini-2.0-flash",
	"gemini-2.5-pro":                 "google/gemini-2.5-pro",
	"gemini-2.5-pro-preview":         "google/gemini-2.5-pro",
	"gemini-2.5-flash":               "google/gemini-2.5-flash",
	"gemini-2.5-flash-preview":       "google/gemini-2.5-flash",
	"gemini-1.5-pro":                 "google/gemini-1.5-pro",
	"gemini-1.5-pro-latest":          "google/gemini-1.5-pro",
	"gemini-1.5-flash":               "google/gemini-1.5-flash",
	"gemini-1.5-flash-latest":        "google/gemini-1.5-flash",
	"gemini-pro":                     "google/gemini-pro",

	// Meta / Llama
	"llama-3.3-70b-instruct":         "meta-llama/llama-3.3-70b-instruct",
	"meta-llama/llama-3.3-70b-instruct": "meta-llama/llama-3.3-70b-instruct",
	"llama-3.1-405b-instruct":        "meta-llama/llama-3.1-405b-instruct",
	"meta-llama/llama-3.1-405b-instruct": "meta-llama/llama-3.1-405b-instruct",
	"llama-3.1-70b-instruct":         "meta-llama/llama-3.1-70b-instruct",
	"llama-3.1-8b-instruct":          "meta-llama/llama-3.1-8b-instruct",

	// Mistral
	"mistral-large":                  "mistralai/mistral-large",
	"mistral-large-latest":           "mistralai/mistral-large",
	"mistral-large-2411":             "mistralai/mistral-large",
	"mistral-small":                  "mistralai/mistral-small",

	// DeepSeek
	"deepseek-v3":                    "deepseek/deepseek-chat",
	"deepseek-chat":                  "deepseek/deepseek-chat",
	"deepseek-r1":                    "deepseek/deepseek-reasoner",
	"deepseek-reasoner":              "deepseek/deepseek-reasoner",
}

// prefixFallbacks maps a lowercased prefix to a slug. Used when exact match
// fails — checked in order of decreasing specificity.
var prefixFallbacks = []struct {
	prefix string
	slug   string
}{
	{"claude-opus-4", "anthropic/claude-opus-4-6"},
	{"claude-sonnet-4", "anthropic/claude-sonnet-4-6"},
	{"claude-3-5-sonnet", "anthropic/claude-3.5-sonnet"},
	{"claude-3-5-haiku", "anthropic/claude-3.5-haiku"},
	{"gpt-4o-mini", "openai/gpt-4o-mini"},
	{"gpt-4o", "openai/gpt-4o"},
	{"gpt-4-turbo", "openai/gpt-4-turbo"},
	{"gemini-2.5-pro", "google/gemini-2.5-pro"},
	{"gemini-2.5-flash", "google/gemini-2.5-flash"},
	{"gemini-2.0-flash", "google/gemini-2.0-flash"},
	{"gemini-1.5-pro", "google/gemini-1.5-pro"},
	{"gemini-1.5-flash", "google/gemini-1.5-flash"},
	{"llama-3.3-70b", "meta-llama/llama-3.3-70b-instruct"},
	{"llama-3.1-405b", "meta-llama/llama-3.1-405b-instruct"},
	{"mistral-large", "mistralai/mistral-large"},
}

// Resolve maps a leaderboard model name to its canonical DB slug.
// Returns ("", false) if no mapping exists.
func Resolve(name string) (slug string, ok bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))

	// Exact match first.
	if s, found := canonicalMap[normalized]; found {
		return s, true
	}

	// Prefix-based fallback for date-suffixed or versioned names.
	for _, pf := range prefixFallbacks {
		if strings.HasPrefix(normalized, pf.prefix) {
			return pf.slug, true
		}
	}

	return "", false
}
