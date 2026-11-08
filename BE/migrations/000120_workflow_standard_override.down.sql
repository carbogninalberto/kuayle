-- Edited Standard workflows become ordinary, archived workflows: tasks keep
-- their pinned versions and new tasks fall back to the built-in flow.
UPDATE workflows SET archived_at = COALESCE(archived_at, now()),
    name = left('Edited Standard ' || left(id::text, 8), 80)
    WHERE builtin_key IS NOT NULL;
DROP INDEX workflows_name;
CREATE UNIQUE INDEX workflows_name ON workflows (workspace_id, lower(name)) WHERE archived_at IS NULL;
DROP INDEX workflows_builtin;
ALTER TABLE workflows DROP COLUMN builtin_key;
