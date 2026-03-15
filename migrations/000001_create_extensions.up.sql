CREATE EXTENSION IF NOT EXISTS timescaledb;
-- pgcrypto provides gen_random_uuid(), used by later migrations.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
