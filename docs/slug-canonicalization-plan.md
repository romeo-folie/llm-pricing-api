# Slug Canonicalization & Backfill — Design Plan

> Status: **PLAN / banked — revisit after PR #184 bakes (~3–5 days)** · Owner: TBD · Created: 2026-06-22
> Follow-up to PR #184 (data-freshness & new-model publishing), which deferred this work.
>
> **Decisions log (2026-06-22):**
> - **Sequencing:** bank the plan; do Phase 0 (observe live pricing after #184) before Phase 1.
> - **D1 (convention):** **variant-first hyphenated** for Anthropic (`claude-opus-4-6`,
>   `claude-fable-5`, `claude-haiku-3-5`, `claude-sonnet-3-7`). See §3.1.
> - D2 (region/tier prefixes), D3 (`-latest`): deferred to Phase 0/2 with live data.

## 1. Problem

The same physical model exists as multiple `models` rows because each source emits a
different slug, and the reconciler groups by `(slug, field)` — so fragments never reconcile,
never reach multi-source agreement, and render as duplicates in the catalog.

Measured on production (2026-06-22, via `/v1/models?provider=`):

| Provider | Rows | Real models | Duplicated identities | Fragment rows |
| --- | --- | --- | --- | --- |
| anthropic | 50 | 30 | 14 | 34 |
| openai | 50 | 43 | 7 | 14 |
| google | 37 | 37 | 0 | 0 |

Representative Anthropic clusters (none currently priced — see §2):

```
claude-opus-4.6: anthropic/claude-4-6-opus | anthropic/claude-opus-4.6 | us/claude-opus-4-6 | fast/claude-opus-4-6 | fast/us/claude-opus-4-6
claude-fable-5:  claude-fable-5 | anthropic/claude-5-fable | anthropic/claude-fable-5
claude-opus-4.8: claude-opus-4-8 | anthropic/claude-4-8-opus | anthropic/claude-opus-4.8
claude-3.5-haiku: anthropic/claude-3-5-haiku | anthropic/claude-3.5-haiku
```

Four distinct fragmentation mechanisms:
1. **Missing provider prefix** — bare `claude-opus-4-8` / `gpt-5.5` (LiteLLM) vs `anthropic/…` / `openai/…`.
2. **Variant-first vs version-first** — `claude-opus-4.8` (OpenRouter verbatim) vs `claude-4-8-opus` (Anthropic scraper).
3. **Dotted vs hyphenated version** — `claude-opus-4.6` vs `claude-4-6-opus`.
4. **Region/tier prefixes** — `us/`, `fast/`, `fast/us/` (Bedrock/Vertex region + latency tier). **May be distinct SKUs.**

## 2. Why this is hard (and why it was deferred from PR #184)

- **There is no single canonical slug convention in the codebase.** `internal/scraper/slugmap`
  (the benchmark-name → slug resolver) is itself inconsistent: `anthropic/claude-3.5-sonnet`
  (version-first, dotted) for 3.x but `anthropic/claude-opus-4-6` (variant-first, hyphenated)
  for 4.x. The Anthropic scraper produces a *third* form (`claude-4-6-opus`). A forward-only
  normalizer that invents a fourth form would diverge pricing rows from the slugs the
  benchmark layer expects.
- **Slugs are a public identifier**, not an internal key:
  - `GET /v1/models/:id` resolves `:id` as slug when non-numeric (`internal/api/handlers/models.go`).
  - Frontend serves `/models/[slug]`, `sitemap.ts`, and `/compare/{a}-vs-{b}` — changing a
    slug is **SEO link rot + broken bookmarks + breaking change for SDK clients**.
  - Benchmark/capability scores attach by slug: `slugmap.Resolve(name)` → `LookupModelIDBySlug`
    → insert. If the canonical slug doesn't exist as a `models` row, the score is silently skipped.
- **All FKs to `models(id)` are `ON DELETE CASCADE`** — deleting a duplicate row cascades away
  its prices/history/scores. The backfill must **re-point before delete**, and handle UNIQUE
  collisions on the destination.
- **Timing**: today the fragments are unpriced (the bug PR #184 fixes). After #184 deploys,
  each source begins pricing the *same* model under its own slug, so we get *multiple priced
  rows per model*. That makes the mess more visible but also gives us live data on which slug
  each source actually uses — useful pre-flight signal. **Let #184 bake for a few days first.**

## 3. Strategy

**Do not invent a new canonical form. Pick a per-model canonical *survivor* — preferring the
slug that is already public / benchmark-attached / indexed — and merge the other fragments
into it.** Then make every source and `slugmap` converge on the survivor going forward, and
add a slug-alias/redirect layer so old slugs keep resolving.

This minimizes breaking changes (the public/indexed slug usually survives) and keeps benchmark
attachment intact.

### 3.1 Canonical convention (DECISION REQUIRED — see §7, D1)

Recommended single rule, applied uniformly and back-fitted into `slugmap`:

- Always `provider/identifier` with a canonical provider token (`anthropic`, `openai`,
  `google`, `meta-llama`, `mistralai`, `deepseek`, …).
- **Anthropic: variant-first, hyphenated** (DECIDED 2026-06-22, D1) →
  `anthropic/claude-<variant>-<major>-<minor>` (e.g. `claude-opus-4-6`, `claude-fable-5`,
  `claude-haiku-3-5`, `claude-sonnet-3-7`). Matches the form `slugmap` already uses for 4.x
  models. **Implications to implement in Phase 1:** (a) the Anthropic scraper's
  `canonicalAnthropicSlug` currently emits *version-first* (`claude-4-6-opus`) and must be
  **flipped to variant-first**; (b) `slugmap`'s 3.x entries (currently dotted version-first,
  e.g. `claude-3.5-sonnet`) must be rewritten to variant-first hyphenated (`claude-sonnet-3-5`).
- Non-Anthropic: provider-native identifier, **dots preserved** (`openai/gpt-4.1`,
  `google/gemini-2.5-pro`), lowercase.
- **Preserved suffixes** (denote different SKUs/pricing): `-fast`, `:thinking`/`-thinking`,
  `-preview`, 8-digit date stamps (`-20241022`).
- **Region prefixes** (`us/`, `eu/`, `fast/us/`): **DECISION REQUIRED (D2)** — strip (same
  model) or keep (distinct SKU). Recommendation: strip *iff* the price matches the un-prefixed
  row; otherwise keep and treat as a separate model. Verify against scraped prices.

> Alternative considered: adopt `slugmap`'s mixed convention as-is. Rejected — its
> inconsistency is the root problem; standardizing once is cheaper than perpetuating it.

## 4. Phased rollout

Ship forward-convergence first (low risk, reversible), then the irreversible backfill as a
separate, snapshot-protected, human-reviewed step.

### Phase 0 — Observe (no code)
- Let PR #184 deploy and run ~3–5 days. Re-run the §1 survey. Capture, per duplicated model,
  **which slug each source publishes a price under** and **whether prices agree** — this is the
  raw material for the merge map and validates the region/tier decision (D2).

### Phase 1 — Forward convergence (low risk, reversible)
Make every new scrape write the canonical survivor slug; nothing existing is mutated.
1. New `internal/scraper/canonical` package: `NormalizeSlug(raw, source) (canonical, changed)`
   + `NormalizeAll(models, source)`. Pure, table-driven, exhaustively tested (mapping table
   from §1 as fixtures). Reuse/extend the normalizer drafted in PR #184's history.
2. Call `canonical.NormalizeAll` once in `worker.runPipeline` between `Fetch` and `EnsureModels`.
3. **Update `slugmap` targets** to the canonical convention so benchmark attachment lands on the
   survivor; update `internal/api/handlers/aliases.go` (the `/v1/ask` alias map, incl. the
   hardcoded `claude-3-5-sonnet-20241022`); update `migrations/000016_seed_benchmark_scores`
   slugs and `testdata/seed.sql`.
4. Add a unit test proving cross-source convergence (OpenRouter + Anthropic + LiteLLM forms of
   one model all normalize to the same survivor) and that benchmark `Resolve` → survivor.
5. Deploy. New canonical rows accumulate multi-source prices and reach `ConfidenceHigh`. The old
   fragment rows stop receiving updates (they go stale) but are not yet removed — handled in Phase 3.
   Monitor for any *new* duplicates (should be zero) and unexpected collisions.

### Phase 2 — Pre-flight merge map (human review gate)
Generate, from a production snapshot, the explicit `old_model_id → canonical_model_id` mapping
and a human-readable report:
- per duplicate group: the chosen survivor, the fragments, **price agreement across fragments**
  (disagreement >5% is a red flag for a wrong merge or a genuine distinct SKU), benchmark-score
  presence, and any region/tier-prefixed members needing the D2 call.
- Surfaced unknowns: `claude-5-mythos` (confirmed *distinct* from fable in the survey — do **not**
  merge), `-latest` pointer aliases (D3), region prefixes (D2).
The map is **hand-reviewed and committed as data**, not auto-derived in SQL, to force review.

### Phase 3 — Backfill migration (irreversible, maintenance window)
Migration `000018_canonicalize_model_slugs` (number TBD at authoring time). Run with:
1. **DB snapshot** (Railway PITR) taken immediately before.
2. **Workers stopped** (pause the worker service) so no scrape writes during the migration —
   the API stays up (separate Railway service).
3. Within one transaction, in this order (re-point **before** any delete; CASCADE is the hazard):

```sql
BEGIN;
CREATE TEMP TABLE _merge(old_id int primary key, new_id int not null);
-- INSERT … hand-filled from the Phase-2 reviewed map.

-- prices: dedupe colliding (model,source) then re-point. UNIQUE(model_id, source_id).
DELETE FROM prices dup USING _merge m WHERE dup.model_id = m.old_id
  AND EXISTS (SELECT 1 FROM prices c WHERE c.model_id = m.new_id AND c.source_id = dup.source_id);
UPDATE prices p SET model_id = m.new_id FROM _merge m WHERE p.model_id = m.old_id;

-- price_history (TimescaleDB hypertable; UPDATE of model_id is allowed — not the partition key).
-- dedup index is (model_id, source_id, confirmed_at, recorded_at): delete true dup tuples first.
DELETE FROM price_history dup USING _merge m WHERE dup.model_id = m.old_id
  AND EXISTS (SELECT 1 FROM price_history c WHERE c.model_id = m.new_id
              AND c.source_id = dup.source_id AND c.confirmed_at = dup.confirmed_at
              AND c.recorded_at = dup.recorded_at);
UPDATE price_history ph SET model_id = m.new_id FROM _merge m WHERE ph.model_id = m.old_id;

-- model_benchmark_scores: UNIQUE(model_id, benchmark_id, benchmark_version).
DELETE FROM model_benchmark_scores dup USING _merge m WHERE dup.model_id = m.old_id
  AND EXISTS (SELECT 1 FROM model_benchmark_scores c WHERE c.model_id = m.new_id
              AND c.benchmark_id = dup.benchmark_id AND c.benchmark_version = dup.benchmark_version);
UPDATE model_benchmark_scores s SET model_id = m.new_id FROM _merge m WHERE s.model_id = m.old_id;

-- model_capability_scores: UNIQUE(model_id, dimension).
DELETE FROM model_capability_scores dup USING _merge m WHERE dup.model_id = m.old_id
  AND EXISTS (SELECT 1 FROM model_capability_scores c WHERE c.model_id = m.new_id AND c.dimension = dup.dimension);
UPDATE model_capability_scores s SET model_id = m.new_id FROM _merge m WHERE s.model_id = m.old_id;

-- review_queue: UNIQUE active index (model_id, field) WHERE status='pending'.
DELETE FROM review_queue dup USING _merge m WHERE dup.model_id = m.old_id AND dup.status = 'pending'
  AND EXISTS (SELECT 1 FROM review_queue c WHERE c.model_id = m.new_id AND c.field = dup.field AND c.status='pending');
UPDATE review_queue r SET model_id = m.new_id FROM _merge m WHERE r.model_id = m.old_id;

-- correct any survivor whose slug isn't yet canonical, then drop the orphans.
UPDATE models SET slug = '<canonical>' WHERE id = <new_id>;   -- per the reviewed map
DELETE FROM models WHERE id IN (SELECT old_id FROM _merge);
COMMIT;
```

4. **`.down.sql` cannot un-merge.** It contains only: `-- No safe rollback. Restore from the
   pre-migration snapshot taken at <ts>.` Rollback = snapshot restore.
5. Restart workers.

### Phase 3b — Old-slug resolution (breaking-change mitigation, ships with Phase 3)
So existing public URLs/bookmarks/benchmark inputs don't 404:
- API: in `GetModelBySlug`, fall back through a committed `old_slug → canonical_slug` alias map
  (and optionally 308-redirect on the frontend `/models/[slug]`). Reuse the Phase-2 map.
- Keep the alias map for ≥1 release cycle; announce in changelog.

### Phase 4 — Verify
- `SELECT canonical, count(*) FROM (…normalize…) GROUP BY 1 HAVING count(*)>1` → **0 rows**.
- Spot-check merged models: continuous price history, benchmark scores intact, `/v1/models/:id`
  resolves for both old and new slug.
- `/v1/models?provider=anthropic` shows one row per real model with `ConfidenceHigh` where ≥2
  sources agree.

## 5. Files in scope (from blast-radius analysis)

| Area | Files |
| --- | --- |
| Normalizer (new) | `internal/scraper/canonical/{canonical.go,canonical_test.go}` |
| Pipeline wiring | `internal/worker/handlers.go` (runPipeline) |
| Benchmark convention | `internal/scraper/slugmap/slugmap.go`, `internal/intelligence/{lookup.go,benchmark_store.go}` |
| Scraper slug output | `internal/scraper/{anthropic,openai,gemini,openrouter,litellm,huggingface}/scraper.go` |
| Hardcoded slugs | `internal/api/handlers/aliases.go`, `migrations/000016_seed_benchmark_scores.up.sql`, `testdata/seed.sql` |
| Backfill | `migrations/000018_canonicalize_model_slugs.{up,down}.sql` + reviewed merge-map |
| Old-slug resolution | `internal/api/handlers/store.go` (GetModelBySlug fallback), `frontend/app/models/[...slug]/page.tsx` |
| FK re-point targets | `prices`, `price_history`, `model_benchmark_scores`, `model_capability_scores`, `review_queue` (all `ON DELETE CASCADE`; `webhooks` has no FK) |

## 6. Risks

| Risk | Severity | Mitigation |
| --- | --- | --- |
| Wrong merge collapses distinct SKUs (`-fast`, `:thinking`, region tiers, dated snapshots) | HIGH | Preserve-suffix list; price-agreement check in pre-flight; hand-reviewed merge map; D2 |
| Benchmark scores orphaned by slug change | HIGH | Update `slugmap` to survivor in Phase 1; re-point scores in Phase 3; verify in Phase 4 |
| Public URL / SEO / SDK breakage | HIGH | Survivor = already-public slug; old-slug alias/redirect (Phase 3b); changelog |
| Irreversible data loss on bad migration | HIGH | Snapshot before; transaction; workers paused; no auto-derived map |
| TimescaleDB chunk locks during `price_history` UPDATE | MED | Maintenance window with workers stopped |
| `claude-5-mythos` / `-latest` misclassified | MED | Pre-flight surfaces them; D3 decision |

## 7. Decisions required before Phase 1

- **D1 — Canonical Anthropic convention. RESOLVED (2026-06-22): variant-first hyphenated**
  (`claude-opus-4-6`, `claude-fable-5`). Flip the Anthropic scraper to variant-first and
  rewrite `slugmap`'s 3.x entries to match (see §3.1).
- **D2 — Region/tier prefixes** (`us/`, `fast/`, `fast/us/`). Strip (same model) or keep
  (distinct SKU)? Driven by whether their scraped price differs from the un-prefixed row.
- **D3 — `-latest` pointer aliases.** Merge into the concrete dated/versioned model, or keep
  as a distinct row?
- **D4 — Old-slug resolution.** API alias-fallback only, or also frontend 308 redirects? How
  long to retain aliases?
- **D5 — Sequencing.** Confirm Phase 0 observation window (recommend 3–5 days post-#184) before
  building the normalizer.

## 8. Effort estimate (rough)

- Phase 1 (normalizer + convergence + slugmap/alias/seed updates + tests): ~1–1.5 days.
- Phase 2 (pre-flight tooling + report + human review): ~0.5 day + review time.
- Phase 3/3b (migration + alias layer + dry-run on a snapshot + window): ~1 day + scheduled window.
- Phase 4 (verification): ~0.5 day.
