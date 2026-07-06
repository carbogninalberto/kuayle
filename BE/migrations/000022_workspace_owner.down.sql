DROP INDEX IF EXISTS idx_workspaces_owner;
ALTER TABLE workspaces DROP COLUMN IF EXISTS owner_id;
