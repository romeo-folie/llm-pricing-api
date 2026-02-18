# migrations

SQL database migrations managed by golang-migrate.

## Purpose

Defines the database schema for the LLM Pricing Platform. Migrations are applied sequentially and each has a corresponding rollback (down) file. The schema uses PostgreSQL with the TimescaleDB extension for time-series price history.

## Structure

```
migrations/
  000001_create_extensions.up.sql      # Enables TimescaleDB extension
  000001_create_extensions.down.sql
  000002_create_sources.up.sql         # Sources table + seed data (7 providers)
  000002_create_sources.down.sql
  000003_create_models.up.sql          # Models table + updated_at trigger
  000003_create_models.down.sql
  000004_create_prices.up.sql          # Current prices with unique constraint
  000004_create_prices.down.sql
  000005_create_price_history.up.sql   # Immutable hypertable (7-day chunks)
  000005_create_price_history.down.sql
  000006_create_review_queue.up.sql    # Discrepancy review queue
  000006_create_review_queue.down.sql
  README.md                            # This file
```

## Key Design Decisions

- **TimescaleDB hypertable** on `price_history` partitioned by `recorded_at` in 7-day chunks for efficient time-range queries.
- **Deduplication index** on `price_history (model_id, source_id, confirmed_at)` prevents duplicate records.
- **Partial unique index** on `review_queue (model_id, field) WHERE status = 'pending'` ensures only one active review per model/field.
- **`set_updated_at()` trigger** on `models` auto-updates `updated_at` on any row change.
- **Seed data** in `000002` pre-populates the 7 known data sources.

## Dependencies

| Dependency | Role |
|---|---|
| PostgreSQL 16 | Database engine |
| TimescaleDB 2.15+ | Time-series extension for `price_history` |
| golang-migrate CLI | Migration runner |

## Usage

```bash
# Install the migrate CLI
make install-tools

# Apply all pending migrations
source .env
make migrate-up

# Roll back the last migration
make migrate-down
```
