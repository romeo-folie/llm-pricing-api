# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Clarifying Questions Before Execution

Before implementing any requested feature or task, always eliminate ambiguity first. Use the `AskUserQuestion` tool for quick targeted questions, or invoke the `/interview` skill for broader requirements gathering. Do not begin writing code until all ambiguities are resolved. Specifically, ask about:

- Scope boundaries (what is explicitly out of scope)
- Edge cases and error handling expectations
- Preferred libraries or patterns if multiple options exist
- Integration points with existing code
- Any constraints (performance, security, backwards compatibility)

## Development Workflow — CCPM

This project uses **CCPM** (Claude Code Project Manager) for task management via GitHub Issues. The `/pm` slash commands are installed in `.claude/commands/pm/`. Run `/pm:help` at any time for a full command listing.

### Phase Lifecycle

#### 1 — Plan the phase

```text
/pm:prd-new <name>          Create a new PRD via brainstorming session
/pm:prd-parse <name>        Convert PRD → technical epic (epic.md + architecture)
/pm:epic-decompose <name>   Break epic → numbered task files with deps & parallelism
/pm:epic-sync <name>        Push epic + tasks to GitHub Issues (creates worktree too)
```

> Shortcut: `/pm:epic-oneshot <name>` runs `epic-decompose` + `epic-sync` in one step.

#### 2 — Start the epic

Choose one mode:

```text
/pm:epic-start <name>           Launch parallel agents in a shared git branch
/pm:epic-start-worktree <name>  Launch parallel agents in a shared git worktree
```

Both modes identify ready issues, run per-issue analysis, and launch agents. Use `epic-start-worktree` when you need full filesystem isolation between sessions.

#### 3 — Work on individual issues

```text
/pm:issue-analyze <N>   Identify parallel work streams before starting
/pm:issue-start <N>     Claim issue, read spec, launch stream agents
/pm:issue-sync <N>      Push local progress as a GitHub comment
/pm:issue-close <N>     Mark complete, close on GitHub, update epic progress
```

#### 4 — Complete the epic

```text
/pm:epic-refresh <name>   Recalculate epic progress from task states
/pm:epic-close <name>     Mark epic complete (validates all tasks are closed first)
/pm:epic-merge <name>     Merge worktree/branch → main, archive, close epic issue
```

---

### Monitoring Commands

```text
/pm:status                  Project-wide dashboard
/pm:next                    Next priority task to work on
/pm:in-progress             All currently active tasks
/pm:blocked                 Blocked tasks and their blockers
/pm:standup                 Daily standup summary
/pm:epic-status <name>      Progress of a specific epic
/pm:epic-show <name>        Full epic details and task list
/pm:issue-status <N>        Issue state (open/closed) and sync status
/pm:issue-show <N>          Full issue detail with sub-issues and activity
```

---

### Editing Commands

```text
/pm:prd-edit <name>         Edit an existing PRD
/pm:epic-edit <name>        Edit epic overview, architecture, or approach
/pm:issue-edit <N>          Edit issue title, description, or labels
/pm:issue-reopen <N>        Reopen a closed issue with a reason
```

---

### Listing & Search

```text
/pm:prd-list                List all PRDs
/pm:prd-status              Status of all PRDs
/pm:epic-list               List all epics
/pm:search <query>          Search PRDs, epics, and tasks by keyword
```

---

### Utility Commands

```text
/pm:sync [name]             Bidirectional sync between local files and GitHub
/pm:import                  Import existing GitHub issues into the PM system
/pm:validate                Validate PM system integrity (file structure + GitHub)
/pm:clean [--dry-run]       Archive completed epics and remove stale files
/pm:init                    Initialize the PM system in a new repository
/pm:help                    Show full command reference
```

### Worktree Safety Protocol

When the `/pm` workflow creates a new worktree (e.g. during `/pm:epic-start-worktree`), immediately perform **both** of the following steps before doing any planning or writing any code:

1. **Write `summary.md` into the worktree root.** This file is the sole context bootstrap for any agent that opens a new session inside the worktree — write it as if the reader has zero prior context. It must be fully populated with live data (not placeholders) and must include every section below.

   **Required sections:**

   - **Header** — phase name, one-sentence description, branch, worktree path, and creation date.
   - **Current Status table** — one row per issue with: issue number, title, current status (`open` / `in-progress` / `done` / `blocked`), and GitHub URL. Populate statuses from the epic task files and GitHub at write time — do not use placeholders.
   - **Next Action** — a single explicit instruction telling the agent exactly what to do first (e.g. "Run `/pm:issue-start 13` to begin the next open task").
   - **Goals** — the phase's key deliverables as a bullet list.
   - **Architecture Notes** — 3–6 bullet points covering decisions and patterns that are *specific to this phase's work*: which packages/directories to work in, which interfaces to implement, which existing modules to integrate with, and any constraints (e.g. "scrapers must never write to `price_history` directly").
   - **Key Files** — a small table of the files/directories most relevant to this phase, with a one-line description of each.
   - **CCPM Quick Commands** — the exact slash commands needed to navigate and complete work in this phase.
   - **Resuming Work** — a numbered checklist an agent follows to pick up where work left off.

   Example (use this as the structural template; populate all fields with real values):

   ````markdown
   # Phase: data-pipeline
   > Implement the scraper, diff engine, and reconciliation engine.

   **Branch:** `epic/data-pipeline` | **Worktree:** `../epic-data-pipeline` | **Created:** 2026-02-18

   ---

   ## Current Status

   | # | Task | Status | GitHub |
   | --- | --- | --- | --- |
   | #12 | OpenRouter scraper goroutine | open | [#12](https://github.com/org/repo/issues/12) |
   | #13 | LiteLLM scraper job | open | [#13](https://github.com/org/repo/issues/13) |
   | #14 | Diff engine | blocked (needs #12, #13) | [#14](https://github.com/org/repo/issues/14) |
   | #15 | Reconciliation engine | blocked (needs #14) | [#15](https://github.com/org/repo/issues/15) |

   **Next action:** Run `/pm:issue-start 12` to begin the OpenRouter scraper.

   ---

   ## Goals
   - Scrape OpenRouter every 6h and LiteLLM daily
   - Diff incoming values against stored values
   - Require 2-source agreement before publishing
   - Write every confirmed change as an immutable timestamped record

   ---

   ## Architecture Notes
   - All scrapers live in `internal/scraper/`; implement the `Scraper` interface (`Fetch() ([]Model, error)`)
   - Use the `asynq` task pattern — register new tasks in `internal/worker/tasks.go`
   - Scrapers must never write to `price_history` directly; all writes go through the reconciliation engine
   - The diff engine compares `[]Model` slices by `model_id`; price deltas >5% must be flagged
   - TimescaleDB hypertable for `price_history` is already created; append-only, no `UPDATE`/`DELETE`

   ---

   ## Key Files

   | Path | Role |
   | --- | --- |
   | `internal/scraper/` | Scraper implementations (primary work area) |
   | `internal/reconciler/` | Reconciliation engine — mediates all writes |
   | `internal/worker/tasks.go` | asynq task registration |
   | `internal/db/migrations/` | Schema migrations |
   | `go.mod` | Module dependencies |

   ---

   ## CCPM Quick Commands

   ```text
   /pm:next                      # What to work on next
   /pm:issue-start <N>           # Begin a task
   /pm:issue-close <N>           # Mark a task complete
   /pm:status                    # Full project dashboard
   /pm:epic-status data-pipeline # This epic's progress
   /pm:blocked                   # See blocked tasks
   ```

   ---

   ## Resuming Work

   1. Check the **Current Status** table above for the next `open` task.
   2. Run `/pm:issue-start <N>` to claim it and read its full spec.
   3. Write tests first, then implement.
   4. Run `go test ./...` — all tests must pass before committing.
   5. Run `/code-reviewer` — fix all findings before closing.
   6. Run `/pm:issue-close <N>` to mark complete and update the status table in this file.

   ````

2. **Pause and prompt the user to switch directories.** Do not proceed with planning, decomposition, or any file writes until the user confirms they have switched their shell to the worktree:

   > "Worktree created at `../epic-<name>`. **Please switch to that directory now** (`cd ../epic-<name>`) and confirm before I continue. Sub-agents will write files relative to your shell's CWD, so they must be launched from inside the worktree."

   Wait for explicit confirmation before continuing.

3. **Keep `summary.md` current.** After each issue is closed, update the status table row for that issue (`done`) and set the new **Next action** line to the next open task. The file must always reflect ground truth — a stale summary defeats its purpose.

### Issue Traceability

All commits and pull requests **must** reference the GitHub Issue they implement. Keep implementation in sync with CCPM issues at all times.

- **Commit messages**: include `#<issue-number>` (e.g. `feat: add diff engine for price reconciliation #12`)
- **Pull requests**: reference the issue in the PR body with `Closes #<issue-number>` or `Resolves #<issue-number>`
- **One issue per branch**: create a feature branch per task, named `<issue-number>-short-description` (e.g. `12-diff-engine`)
- After completing work, use `/pm:issue-close <issue-number>` to mark the task done — do not close issues without a corresponding commit or PR

## Documentation

Documentation is a **top-priority, first-class deliverable** — not an afterthought. Agents operate with significant autonomy in this repository, so the codebase must be self-explanatory to any reader without prior context.

### Module READMEs

Every module (i.e. every package under `internal/`, `cmd/`, `mcp/`, `frontend/`, and any other top-level directory containing code) **must** have a `README.md` that includes:

- **Purpose** — what problem this module solves and where it fits in the architecture.
- **Structure** — a directory/file tree annotated with a one-line description of each file's role.
- **Key components** — a short prose explanation of the main types, functions, or subsystems and how they interact.
- **Dependencies** — which other modules or external services this module depends on.
- **Usage** — example invocations, configuration, or integration notes relevant to the module.

### Keeping Docs in Sync

After every feature or task that touches a module:

1. Review the module's `README.md` against the current code.
2. Update any sections that are stale, incomplete, or missing.
3. Add entries for any new files, types, or significant functions introduced.
4. The README update must be included in the same commit as the code change — never deferred.

A feature is **not complete** until the README for every touched module accurately reflects the post-change state of the code.

## Code Review Gate

After implementing any feature or task, you **must** run the `/code-reviewer` skill before reporting the task as complete:

1. Invoke `/code-reviewer` on the changed code using **Sonnet** as the model.
2. Fix every issue and test recommendation it identifies.
3. Re-run `/code-reviewer` on the fixes using **Opus** as the model.
4. Fix any additional findings from the Opus pass.
5. Repeat with Opus until the review comes back clean with no actionable findings.
6. Only then may you mark the task complete or close the issue.

If a flagged issue is intentionally skipped (e.g. out of scope, deferred, won't-fix), you **must** state the reason in the task completion summary. Do not silently skip findings. Every skipped item needs a one-line justification alongside it.

A task is **not done** until the code-reviewer confirms it is clean or all remaining findings have documented skip reasons.

## Pre-Commit Checklist

Before creating any commit, the following must all pass:

1. **All tests green**: `go test ./...` exits with zero failures.
2. **Build succeeds**: `go build ./...` (and `cd frontend && npm run build` / `cd mcp && npm run build` for frontend/MCP changes) completes without errors.
3. **Code review clean**: `/code-reviewer` returns no actionable findings (see Code Review Gate above).

Do not commit if any of the above fail.

## Test-Driven Development

This project follows a **test-driven development (TDD)** approach for all core modules. Tests are a first-class priority, not an afterthought.

- **Write tests before implementation**. For each unit of work, write failing tests that define the expected behaviour, then write the code to make them pass.
- **No feature is complete without tests**. A task cannot be closed (via `/pm:issue-close`) unless its tests pass.
- **Test the reconciliation engine thoroughly** — this is the critical data integrity boundary. Cover: discrepancy detection, threshold logic, 2-source agreement, flagging, and immutable history writes.
- **Integration tests for API endpoints** — cover auth, tier gating, filtering, pagination, error responses (RFC 7807), and trust metadata presence.
- **Run tests before committing**: `go test ./...` must pass with zero failures before any commit.

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/reconciler/...

# Run a single test by name
go test -run TestReconcile ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...
```

## What This Project Is

LLM Token Pricing Platform — a reconciled, multi-source pricing data API for LLM models. It aggregates pricing from OpenRouter, LiteLLM, and provider docs, reconciles discrepancies, stores immutable price history in TimescaleDB, and serves it through a versioned REST API and MCP server.

The differentiator is **price history + change tracking**. Every competitor gives a snapshot; this gives the full timeline and sells reliable programmatic access to it.

## Architecture Overview

The system has six layers, built in this order:

1. **Data Pipeline** — Go goroutines + asynq cron workers scrape three sources (OpenRouter every 6h, LiteLLM daily, provider docs daily). A diff engine compares incoming values against stored values. A reconciliation engine requires 2-source agreement to publish; discrepancies >5% are flagged to a review queue. Every confirmed change is written as an immutable timestamped record.

2. **Storage** — PostgreSQL + TimescaleDB for price history (immutable time-series records). Redis for job queue (asynq), response caching, and rate limiting.

3. **REST API** — Go + Fiber serving `/v1/` endpoints. Auth via Unkey API keys with tier-based gating (Free/Developer/Pro). All responses use a consistent JSON envelope with trust metadata (`confirmed_at`, `source`, `confidence`, `age_hours`, `change_velocity`). Errors follow RFC 7807.

4. **Agent Interface** — MCP server (`@llmpricing/mcp` on npm, TypeScript), SSE stream at `/v1/stream/changes`, natural language query at `/v1/ask`, context snapshot at `/v1/context` (~2k tokens), and discovery endpoints (`/openapi.json`, `/.well-known/ai-plugin.json`, `/llms.txt`).

5. **Frontend** — Next.js with TypeScript and Tailwind. SSR for SEO. Comparison table, cost calculator, price history charts (Tremor or Recharts), model recommender UI.

6. **Monetisation** — Lemon Squeezy as Merchant of Record. Three tiers: Free ($0, 100 req/day), Developer ($15, 10k req/day), Pro ($50, unlimited + webhooks + SLA).

## API Endpoints

| Endpoint | Tier | Purpose |
| --- | --- | --- |
| `GET /v1/models` | Free | List models with filters: `?provider=`, `?modality=`, `?min_context=` |
| `GET /v1/models/:id` | Free | Single model detail |
| `GET /v1/models/:id/history` | Dev+ | Price history with `?from=`, `?to=` |
| `GET /v1/compare?models=` | Free | Compare up to 5 models |
| `GET /v1/recommend` | Dev+ | Ranked models by task/context/price |
| `GET /v1/providers` | Free | Provider list |
| `GET /v1/changes` | Free | Recent price changes with `?since=`, `?provider=` |
| `POST /v1/webhooks` | Pro | Register webhook |
| `DELETE /v1/webhooks/:id` | Pro | Remove webhook |
| `GET /v1/context` | Dev+ | ~2k token pricing snapshot for agent system prompts |
| `POST /v1/ask` | Dev+ | NL query → structured response with `inferred_params` |
| `GET /v1/stream/changes` | Dev+ | SSE stream with reconnection via `Last-Event-ID` |

## Key Technical Decisions

- **Reconciliation before publishing**: price data is never written directly from a scraper. The reconciliation engine mediates all writes. Single-source changes require 2 consecutive matching fetches. Multi-source disagreements are held in a review queue.
- **Immutable history**: `price_history` records are append-only. No in-place updates. Every record has source attribution.
- **Trust metadata on every response**: `confirmed_at`, `source`, `confidence` (high/medium/low), `age_hours`, `change_velocity`. Agents use these to decide whether to trust a value.
- **Tier gating via Unkey middleware**: Fiber middleware validates the API key, extracts the tier, and attaches it to the request context. Cache Unkey validation in Redis with 30s TTL.
- **Webhook delivery**: via asynq jobs, at-least-once with 3 retries and exponential backoff.

## File Reading

When reading `.docx` files, use the Read tool directly (it supports binary/image reading) rather than falling back to a Python extraction script. Only use a Python-based extraction approach if the Read tool fails to return usable content.

## Build & Run Commands

```bash
# Go API
go build -o bin/api ./cmd/api
go run ./cmd/api

# Background workers
go run ./cmd/worker

# Frontend (Next.js)
cd frontend && npm install
cd frontend && npm run dev
cd frontend && npm run build

# MCP server
cd mcp && npm install
cd mcp && npm run build
npx @llmpricing/mcp
```

## Stack Reference

| Component | Technology |
| --- | --- |
| API server | Go + Fiber |
| Database | PostgreSQL + TimescaleDB |
| Job queue / cache | Redis + asynq |
| Frontend | Next.js + TypeScript + Tailwind |
| Charts | Tremor or Recharts |
| MCP server | TypeScript + MCP SDK |
| API key management | Unkey |
| Payments | Lemon Squeezy |
| Hosting | Railway |

## Data Sources

| Source | Frequency | Role |
| --- | --- | --- |
| OpenRouter `/v1/models` | Every 6 hours | Primary feed |
| LiteLLM model cost map (GitHub JSON) | Daily | Cross-reference |
| Provider docs (OpenAI, Anthropic, Google, Mistral, Amazon) | Daily scrape | Ground truth |

## Reconciliation Rules

- Any source disagreement >5% → flag for manual review (4hr SLA during working hours)
- Single-source change → auto-publish after 2 consecutive matching fetches
- Flagged records never silently resolve — require confirmed match or manual override
- Every confirmed change → immutable record in `price_history` with source attribution

## Performance Targets

- API p99 latency: <200ms for all read endpoints
- `/v1/context` response: ≤2,100 tokens (verified with tiktoken)
- Webhook delivery: at-least-once, max 3 attempts over 15 minutes
- SSE heartbeat: every 30 seconds
- Data freshness: no published value older than 24 hours without a stale indicator
