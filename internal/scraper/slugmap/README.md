# internal/scraper/slugmap

Canonical model-slug resolution for benchmark leaderboard names.

## Purpose

Benchmark leaderboards identify models with free-form display names — `"gpt-4o-2024-08-06"`, `"Claude 3.5 Sonnet (20241022)"`, `"o3-20250416"` — while the database keys everything on canonical slugs like `openai/gpt-4o`. This package is the single allowlisted mapping between the two.

It exists to prevent the worst failure mode in benchmark ingestion: **attributing one model generation's score to another**. A fuzzy matcher that maps `claude-3-5-sonnet` onto `claude-3-sonnet` would silently corrupt capability scores in a way no test of the surrounding pipeline would catch.

## Structure

```
internal/scraper/slugmap/
  slugmap.go       # canonicalMap table and Resolve
  slugmap_test.go  # Exact/prefix/case tests, plus generation-crossing regression tests
  README.md        # This file
```

## Key Components

### `Resolve`

```go
func Resolve(name string) (slug string, ok bool)
```

Resolution order:

1. **Exact match** on the lowercased name against `canonicalMap`.
2. **Contains-based fallback** for clearly delimited date/variant suffixes.
3. **`ok == false`** if neither matches — the caller logs and skips the entry.

Returning `false` rather than a best guess is the entire point. An unresolved leaderboard row is dropped, never approximated.

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
slug, ok := slugmap.Resolve(entry.ModelName)
if !ok {
    s.logger.Debug().Str("model", entry.ModelName).Msg("unresolved leaderboard model; skipping")
    continue
}
// slug is now safe to use as a DB key
```

## Design Notes

- **Allowlist, not heuristic.** Adding support for a new model means adding a row to `canonicalMap`. There is no scoring, edit-distance, or embedding similarity — all of those can cross generations.
- **Generation crossing is a tested invariant.** `TestResolve_DoesNotCrossClaudeGenerations` exists because Claude names are the most collision-prone family; keep it passing when extending the table.
- **Case-insensitive** by lowercasing the input before lookup.
- **Pure and dependency-free.** No I/O, no database, no config — imports `strings` only, so it is trivially testable and safe to call in a loop.

## Dependencies

Standard library only (`strings`).

Consumed by the benchmark scrapers [`swebench`](../swebench/README.md) and [`livecodebench`](../livecodebench/README.md). See also `docs/slug-canonicalization-plan.md`.
