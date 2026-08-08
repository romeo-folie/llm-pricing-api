# migrations

SQL database migrations managed by golang-migrate.

## Purpose

Defines the database schema for the LLM Pricing Platform. Migrations are applied sequentially and each has a corresponding rollback (down) file. The schema uses PostgreSQL with TimescaleDB for time-series price history and `pgcrypto` for UUID generation.

## Structure

```
migrations/
  000001_create_extensions.up.sql      # Enables TimescaleDB and pgcrypto extensions
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
  000007_create_webhooks.up.sql        # Webhooks table; guards pgcrypto for gen_random_uuid()
  000007_create_webhooks.down.sql
  000008_huggingface_source.up.sql      # Adds HuggingFace as a source
  000008_huggingface_source.down.sql
  000009_update_openai_source_url.up.sql  # Updates OpenAI source URL
  000009_update_openai_source_url.down.sql
  000010_restore_anthropic_source.up.sql  # Restores Anthropic source row
  000010_restore_anthropic_source.down.sql
  000011_restore_google_source.up.sql  # Restores Google source row
  000011_restore_google_source.down.sql
  000012_create_identity_tables.up.sql # Identity/signup tables (api_identities,
                                       # magic_link_tokens, api_keys_registry)
  000012_create_identity_tables.down.sql
  000013_create_benchmarks.up.sql      # Benchmark catalogue
  000014_create_model_benchmark_scores.up.sql      # Per-model benchmark scores
  000015_create_model_capability_scores.up.sql     # Per-model capability scores
  000016_seed_benchmark_scores.up.sql  # Seed benchmark/capability data
  000017_add_last_verified_at.up.sql   # Adds prices.last_verified_at (freshness anchor)
  000017_add_last_verified_at.down.sql
  000018_add_benchmark_provenance.up.sql   # Upstream identities + active-evidence index
  000018_add_benchmark_provenance.down.sql
  README.md                            # This file
```

> **Note:** The list above reflects the full current migration chain. If new migrations are added, update this README accordingly.

## Key Design Decisions

- **TimescaleDB hypertable** on `price_history` partitioned by `recorded_at` in 7-day chunks for efficient time-range queries.
- **Deduplication index** on `price_history (model_id, source_id, confirmed_at, recorded_at)` prevents duplicate records. The `recorded_at` column is included because TimescaleDB requires the partition key in every unique index on a hypertable.
- **Partial unique index** on `review_queue (model_id, field) WHERE status = 'pending'` ensures only one active review per model/field.
- **`set_updated_at()` trigger** on `models` (and `api_identities`) auto-updates `updated_at` on any row change. The trigger function is defined once in `000003` and reused by later migrations.
- **Seed data** in `000002` pre-populates the 7 known data sources.
- **`pgcrypto` extension** is enabled in `000001` for fresh installs, guarded in `000007` (first migration using `gen_random_uuid()`) and `000012` for existing DBs that applied earlier migrations before pgcrypto was added. All declarations are `IF NOT EXISTS` and fully idempotent.
- **Identity/signup subsystem** (`000012`) introduces `api_identities`, `magic_link_tokens`, and `api_keys_registry` to support the free-key onboarding flow. Email normalisation, token expiry, and key-status consistency are enforced at the DB level via CHECK constraints.

## Dependencies

| Dependency | Role |
|---|---|
| PostgreSQL 16 | Database engine |
| TimescaleDB 2.15+ | Time-series extension for `price_history` |
| pgcrypto | `gen_random_uuid()` defaults for UUID primary keys |
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
