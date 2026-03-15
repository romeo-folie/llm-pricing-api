-- Rollback migration 000012: drop identity tables (cascade drops indexes/triggers)
DROP TABLE IF EXISTS api_keys_registry;
DROP TABLE IF EXISTS magic_link_tokens;
DROP TABLE IF EXISTS api_identities;
DROP FUNCTION IF EXISTS set_updated_at();
