# internal/reconciler

Price reconciliation engine.

## Purpose

Mediates all price writes to the database. Compares incoming scraped values against stored values, enforces 2-source agreement before publishing, flags discrepancies >5% to the review queue, and writes immutable records to `price_history`. This is the critical data integrity boundary of the system.

## Structure

```
internal/reconciler/
  README.md    # This file
```

Currently a placeholder — implementation will be added in Phase 1.

## Dependencies (planned)

| Dependency | Role |
|---|---|
| `internal/models` | Domain structs for prices, history, review items |
| `internal/database` | PostgreSQL pool for reads and writes |

## Reconciliation Rules

- Single-source change: auto-publish after 2 consecutive matching fetches
- Multi-source disagreement >5%: flag for manual review (4hr SLA)
- Flagged records never silently resolve — require confirmed match or manual override
- Every confirmed change: immutable record in `price_history` with source attribution
