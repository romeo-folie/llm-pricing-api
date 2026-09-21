# internal/scraper/slugmap

Canonical model-slug resolution for benchmark leaderboard names.

## Purpose

Benchmark leaderboards identify models with free-form display names — `"gpt-4o-2024-08-06"`, `"Claude 3.5 Sonnet (20241022)"`, `"o3-20250416"` — while the database keys everything on canonical slugs like `openai/gpt-4o`. This package is the single allowlisted mapping between the two.

It exists to prevent the worst failure mode in benchmark ingestion: **attributing one model generation's score to another**. A fuzzy matcher that maps `claude-3-5-sonnet` onto `claude-3-sonnet` would silently corrupt capability scores in a way no test of the surrounding pipeline would catch.

## Structure

```
internal/scraper/slugmap/
  slugmap.go       # canonicalMap table, Resolve and ResolveOutcome
  slugmap_test.go  # Exact/prefix/case tests, generation-crossing regression tests, ambiguous-outcome tests
  README.md        # This file
```

## Key Components

### `Resolve`

```go
func Resolve(name string) (slug string, ok bool)
```

Resolution order:

1. **Exact match** on the lowercased name against `canonicalMap`.
2. **Prefix fallback** for clearly delimited date/variant suffixes.
3. **`ok == false`** if neither matches — the caller logs and skips the entry.

Returning `false` rather than a best guess is the entire point. An unresolved leaderboard row is dropped, never approximated. `Resolve` is now a thin wrapper over `ResolveOutcome`, so its behaviour is unchanged.

### `ResolveOutcome`

```go
type Outcome string
const (
    OutcomeResolved  Outcome = "resolved"
    OutcomeUnknown   Outcome = "unknown"
    OutcomeAmbiguous Outcome = "ambiguous"
)

func ResolveOutcome(name string) (slug string, outcome Outcome)
```

The same resolution, but it reports *why* a name matched or did not, because `Resolve`'s boolean collapses "the allowlist has no row for this name" and "more than one row matched" into a single `ok == false`. Benchmark coverage cannot be audited without that distinction: `unknown` means upstream is publishing models the allowlist does not know, while `ambiguous` means the allowlist is under-specified for a name it already half-knows.

Ambiguity is real, not hypothetical. `prefixFallbacks` is ordered by decreasing specificity and **more than one rule can match the same name** — `gpt-4o-mini-20250101` matches both `gpt-4o-mini` and `gpt-4o`, and `gpt-4.1-mini-20250101` matches both `gpt-4.1-mini` and `gpt-4.1`. Today the first entry in the list wins by convention.

When two matching rules disagree on the slug, `ResolveOutcome` returns the **greedy first match together with `OutcomeAmbiguous`**. Returning an empty slug instead would make `Resolve` start rejecting names it accepts today and silently shrink benchmark coverage on the very deploy that added observability for it. Callers that care can branch on the outcome; callers that do not are unaffected. Two matching rules that agree on the same slug are `resolved` — the name is under-specified but the answer is not.

### `canonicalMap`

A hand-maintained table of lowercased leaderboard names → canonical DB slugs, grouped by provider. Multiple keys intentionally collapse to one slug so that dated snapshot names land on the same model:

```go
"gpt-4o":            "openai/gpt-4o",
"gpt-4o-2024-05-13": "openai/gpt-4o",
"gpt-4o-2024-08-06": "openai/gpt-4o",
"gpt-4o-2024-11-20": "openai/gpt-4o",
```

## Usage

```go
slug, outcome := slugmap.ResolveOutcome(entry.ModelName)
if outcome == slugmap.OutcomeUnknown {
    s.logger.Debug().Str("model", entry.ModelName).Msg("unresolved leaderboard model; skipping")
    continue
}
// slug is now safe to use as a DB key. outcome also reports OutcomeAmbiguous,
// in which case slug is the greedy first match.
```

The benchmark scrapers call this through a small per-package helper that also
increments `llm_slug_resolutions_total{result}` once per leaderboard entry, so the
resolver's outcome split is visible in production:

```go
func resolveSlug(name string) (string, slugmap.Outcome) {
    slug, outcome := slugmap.ResolveOutcome(name)
    metrics.SlugResolutionsTotal.WithLabelValues(outcome.String()).Inc()
    return slug, outcome
}
```

## Design Notes

- **Allowlist, not heuristic.** Adding support for a new model means adding a row to `canonicalMap`. There is no scoring, edit-distance, or embedding similarity — all of those can cross generations.
- **Generation crossing is a tested invariant.** `TestResolve_DoesNotCrossClaudeGenerations` exists because Claude names are the most collision-prone family; keep it passing when extending the table.
- **Case-insensitive** by lowercasing the input before lookup.
- **Pure and dependency-free.** No I/O, no database, no config, no metrics registry — imports `strings` only, so it is trivially testable and safe to call in a loop. The `llm_slug_resolutions_total` counter is incremented by the *callers*, not here, precisely to keep it that way.
- **Adding a name must not change an existing answer.** `ResolveOutcome` returns the greedy first match for ambiguous names, and `TestResolve_AmbiguousStaysBackwardsCompatible` pins that; the ambiguous set is expected to shrink as specific entries are added to `canonicalMap`, never by breaking resolution.

## Dependencies

Standard library only (`strings`).

Consumed by the benchmark scrapers [`swebench`](../swebench/README.md) and [`livecodebench`](../livecodebench/README.md). See also `docs/slug-canonicalization-plan.md`.
