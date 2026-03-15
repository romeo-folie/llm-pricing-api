-- TimescaleDB extension is intentionally not dropped to avoid data loss.
-- To remove: DROP EXTENSION timescaledb CASCADE;

-- pgcrypto extension is intentionally not dropped on rollback.
-- It may be used by other extensions or future migrations.
-- To remove: DROP EXTENSION IF EXISTS pgcrypto CASCADE;
