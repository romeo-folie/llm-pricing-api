-- Rollback migration 000012: drop identity tables (cascade drops indexes/triggers)
-- set_updated_at() is shared infrastructure created by migration 000003;
-- it is intentionally NOT dropped here to avoid breaking other tables' triggers.
DROP TABLE IF EXISTS api_keys_registry;
DROP TABLE IF EXISTS magic_link_tokens;
DROP TABLE IF EXISTS api_identities;
