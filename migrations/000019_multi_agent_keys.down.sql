-- Rollback migration 000019: restore one-active-key-per-identity, drop grants.
-- Order matters: the unique index can only be recreated after collapsing any
-- identity that accumulated multiple active keys under the multi-key schema.

DROP TABLE IF EXISTS agent_grants;

-- Keep the newest active key per identity and revoke the rest, so the partial
-- unique index below can be recreated without violating itself. Tie-break on id
-- so the choice is deterministic when created_at values collide.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY identity_id
               ORDER BY created_at DESC, id DESC
           ) AS rn
    FROM api_keys_registry
    WHERE status = 'active'
)
UPDATE api_keys_registry AS k
SET status = 'revoked', revoked_at = NOW()
FROM ranked AS r
WHERE k.id = r.id AND r.rn > 1;

DROP INDEX IF EXISTS idx_api_keys_registry_active_by_identity;

CREATE UNIQUE INDEX idx_api_keys_registry_one_active_per_identity
    ON api_keys_registry (identity_id)
    WHERE status = 'active';

ALTER TABLE api_keys_registry
    DROP CONSTRAINT IF EXISTS api_keys_registry_label_len,
    DROP CONSTRAINT IF EXISTS api_keys_registry_created_via_valid;

ALTER TABLE api_keys_registry
    DROP COLUMN IF EXISTS created_via,
    DROP COLUMN IF EXISTS label;
