// Package slugmap provides canonical slug resolution for model names
// as returned by external benchmark leaderboards.
package slugmap

import "strings"

// canonicalMap maps lowercased leaderboard model names (or partial names)
// to their canonical DB slugs. Exact matches are tried first; if none match,
// a contains-based fallback is used.
var canonicalMap = map[string]string{
	// OpenAI
	"gpt-4o":                     "openai/gpt-4o",
	"gpt-4o-2024-05-13":          "openai/gpt-4o",
	"gpt-4o-2024-08-06":          "openai/gpt-4o",
	"gpt-4o-2024-11-20":          "openai/gpt-4o",
	"gpt-4o-mini":                "openai/gpt-4o-mini",
	"gpt-4o-mini-2024-07-18":     "openai/gpt-4o-mini",
	"gpt-4-turbo":                "openai/gpt-4-turbo",
	"gpt-4-turbo-2024-04-09":     "openai/gpt-4-turbo",
	"gpt-4-1":                    "openai/gpt-4.1",
	"gpt-4.1":                    "openai/gpt-4.1",
	"gpt-4.1-mini":               "openai/gpt-4.1-mini",
	"gpt-4.1-nano":               "openai/gpt-4.1-nano",
	"o1":                         "openai/o1",
	"o1-preview":                 "openai/o1-preview",
	"o1-mini":                    "openai/o1-mini",
	"o3":                         "openai/o3",
	"o1-2024-12-17":              "openai/o1",
	"o3-mini":                    "openai/o3-mini",
	"o3-20250416":                "openai/o3",
	"o3__high":                   "openai/o3",
	"o3-mini-2025-01-31__low":    "openai/o3-mini",
	"o3-mini-2025-01-31__medium": "openai/o3-mini",
	"o3-mini-2025-01-31__high":   "openai/o3-mini",
	"o4-mini":                    "openai/o4-mini",
	"o4-mini-20250416":           "openai/o4-mini",
	"o4-mini__high":              "openai/o4-mini",
	"o4-mini__medium":            "openai/o4-mini",
	"o4-mini__low":               "openai/o4-mini",

	// Anthropic
	"claude-3-5-sonnet":          "anthropic/claude-3-5-sonnet",
	"claude-3-5-sonnet-20241022": "anthropic/claude-3-5-sonnet",
	"claude-3-5-sonnet-20240620": "anthropic/claude-3-5-sonnet",
	"claude-3.5-sonnet":          "anthropic/claude-3-5-sonnet",
	"claude-3-5-haiku":           "anthropic/claude-3-5-haiku",
	"claude-3-5-haiku-20241022":  "anthropic/claude-3-5-haiku",
	"claude-3.5-haiku":           "anthropic/claude-3-5-haiku",
	"claude-3-7-sonnet-20250219": "anthropic/claude-3.7-sonnet",
	"claude-sonnet-4-20250514":   "anthropic/claude-sonnet-4",
	"claude-4-sonnet-20250514":   "anthropic/claude-sonnet-4",
	"claude-opus-4-20250514":     "anthropic/claude-opus-4",
	"claude-4-opus-20250514":     "anthropic/claude-opus-4",
	"claude-opus-4-6":            "anthropic/claude-opus-4.6",
	"claude-sonnet-4-6":          "anthropic/claude-sonnet-4.6",
	"claude-sonnet-4-5":          "anthropic/claude-sonnet-4.5",
	"claude-sonnet-4-5-20250929": "anthropic/claude-sonnet-4.5",
	"claude-sonnet-4.5":          "anthropic/claude-sonnet-4.5",
	"claude-opus-4-5":            "anthropic/claude-opus-4.5",
	"claude-opus-4-5-20251101":   "anthropic/claude-opus-4.5",
	"claude-4-5-opus":            "anthropic/claude-opus-4.5",
	"claude-haiku-4-5-20251001":  "anthropic/claude-haiku-4.5",
	"claude-3-opus":              "anthropic/claude-3-opus",
	"claude-3-opus-20240229":     "anthropic/claude-3-opus",
	"claude-3-haiku":             "anthropic/claude-3-haiku",
	"claude-3-haiku-20240307":    "anthropic/claude-3-haiku",

	// Google
	"gemini-2.0-flash":         "google/gemini-2.0-flash",
	"gemini-2.0-flash-001":     "google/gemini-2.0-flash",
	"gemini-2.5-pro":           "google/gemini-2.5-pro",
	"gemini-2.5-pro-preview":   "google/gemini-2.5-pro",
	"gemini-2.5-flash":         "google/gemini-2.5-flash",
	"gemini-2.5-flash-preview": "google/gemini-2.5-flash",
	"gemini-1.5-pro":           "google/gemini-1.5-pro",
	"gemini-1.5-pro-latest":    "google/gemini-1.5-pro",
	"gemini-1.5-flash":         "google/gemini-1.5-flash",
	"gemini-1.5-flash-latest":  "google/gemini-1.5-flash",
	"gemini-pro":               "google/gemini-pro",

	// Meta / Llama
	"llama-3.3-70b-instruct":             "meta-llama/llama-3.3-70b-instruct",
	"meta-llama/llama-3.3-70b-instruct":  "meta-llama/llama-3.3-70b-instruct",
	"llama-3.1-405b-instruct":            "meta-llama/llama-3.1-405b-instruct",
	"meta-llama/llama-3.1-405b-instruct": "meta-llama/llama-3.1-405b-instruct",
	"llama-3.1-70b-instruct":             "meta-llama/llama-3.1-70b-instruct",
	"llama-3.1-8b-instruct":              "meta-llama/llama-3.1-8b-instruct",

	// Mistral
	"mistral-large":        "mistralai/mistral-large",
	"mistral-large-latest": "mistralai/mistral-large",
	"mistral-large-2411":   "mistralai/mistral-large",
	"mistral-small":        "mistralai/mistral-small",

	// DeepSeek
	"deepseek-v3":       "deepseek/deepseek-chat",
	"deepseek-chat":     "deepseek/deepseek-chat",
	"deepseek-r1":       "deepseek/deepseek-reasoner",
	"deepseek-reasoner": "deepseek/deepseek-reasoner",
}

// prefixFallbacks maps a lowercased prefix to a slug. Used when exact match
// fails — checked in order of decreasing specificity.
var prefixFallbacks = []struct {
	prefix string
	slug   string
}{
	{"claude-opus-4-5", "anthropic/claude-opus-4.5"},
	{"claude-sonnet-4-5", "anthropic/claude-sonnet-4.5"},
	{"claude-3-7-sonnet", "anthropic/claude-3.7-sonnet"},
	{"claude-3-5-sonnet", "anthropic/claude-3-5-sonnet"},
	{"claude-3-5-haiku", "anthropic/claude-3-5-haiku"},
	{"gpt-4o-mini", "openai/gpt-4o-mini"},
	{"gpt-4o", "openai/gpt-4o"},
	{"gpt-4-turbo", "openai/gpt-4-turbo"},
	{"gpt-4.1-mini", "openai/gpt-4.1-mini"},
	{"gpt-4.1", "openai/gpt-4.1"},
	{"o4-mini", "openai/o4-mini"},
	{"o3-mini", "openai/o3-mini"},
	{"gemini-2.5-pro", "google/gemini-2.5-pro"},
	{"gemini-2.5-flash", "google/gemini-2.5-flash"},
	{"gemini-2.0-flash", "google/gemini-2.0-flash"},
	{"gemini-1.5-pro", "google/gemini-1.5-pro"},
	{"gemini-1.5-flash", "google/gemini-1.5-flash"},
	{"llama-3.3-70b", "meta-llama/llama-3.3-70b-instruct"},
	{"llama-3.1-405b", "meta-llama/llama-3.1-405b-instruct"},
	{"mistral-large", "mistralai/mistral-large"},
}

// Outcome describes how a leaderboard model name was classified by
// ResolveOutcome. It exists so the benchmark pipeline can tell "the allowlist
// has no entry for this name" apart from "more than one allowlist rule matched
// it" — a distinction Resolve's boolean collapses into a single failure.
type Outcome string

const (
	// OutcomeResolved means exactly one canonical slug was matched.
	OutcomeResolved Outcome = "resolved"
	// OutcomeUnknown means no allowlist entry matched the name.
	OutcomeUnknown Outcome = "unknown"
	// OutcomeAmbiguous means more than one prefix rule matched the name with
	// different canonical slugs. The greedy first-match slug is still returned
	// (see ResolveOutcome) so existing callers keep their behaviour, but the
	// outcome records that the match was not unique.
	OutcomeAmbiguous Outcome = "ambiguous"
)

// String returns the outcome as its metric-label value.
func (o Outcome) String() string { return string(o) }

// ResolveOutcome maps a leaderboard model name to its canonical DB slug and
// reports how the match was made.
//
// Resolution order is unchanged from the original Resolve:
//
//  1. Exact match on the lowercased name against canonicalMap → OutcomeResolved.
//  2. Prefix fallback for date/variant suffixes. If every matching prefix rule
//     agrees on a slug the result is OutcomeResolved; if two rules disagree the
//     outcome is OutcomeAmbiguous and the first match — the order of
//     prefixFallbacks is the specificity convention — is returned.
//  3. No match at all → OutcomeUnknown with an empty slug.
//
// The greedy first match is returned for ambiguous names deliberately: the
// resolver must not start rejecting names it used to accept, because that would
// shrink benchmark coverage on the deploy that added observability for it.
// Callers that care can branch on the outcome instead.
func ResolveOutcome(name string) (slug string, outcome Outcome) {
	normalized := strings.ToLower(strings.TrimSpace(name))

	// Exact match first.
	if s, found := canonicalMap[normalized]; found {
		return s, OutcomeResolved
	}

	// Prefix-based fallback for date-suffixed or versioned names.
	var first string
	matched := false
	for _, pf := range prefixFallbacks {
		if !hasKnownVariantPrefix(normalized, pf.prefix) {
			continue
		}
		if !matched {
			first = pf.slug
			matched = true
			continue
		}
		if pf.slug != first {
			// Two rules matched with different slugs: the name is
			// under-specified for the allowlist (for example "gpt-4o-mini-x"
			// matches both "gpt-4o-mini" and "gpt-4o"). Fall through with the
			// first match so behaviour is unchanged.
			return first, OutcomeAmbiguous
		}
	}
	if !matched {
		return "", OutcomeUnknown
	}
	return first, OutcomeResolved
}

// Resolve maps a leaderboard model name to its canonical DB slug.
// Returns ("", false) if no mapping exists. Ambiguous names still return the
// greedy first-match slug with ok=true, exactly as before this variant existed.
//
// It is a thin wrapper over ResolveOutcome so existing callers and tests are
// unaffected. New callers that need to distinguish "no match" from "ambiguous
// match" should call ResolveOutcome.
func Resolve(name string) (slug string, ok bool) {
	slug, outcome := ResolveOutcome(name)
	return slug, outcome != OutcomeUnknown
}

func hasKnownVariantPrefix(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	switch name[len(prefix)] {
	case '-', '_':
		return true
	default:
		return false
	}
}
