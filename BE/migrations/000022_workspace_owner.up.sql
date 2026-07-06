-- Track the workspace owner directly on the workspaces table.
-- Backfill from workspace_members (each workspace is created with exactly one owner).
ALTER TABLE workspaces ADD COLUMN owner_id UUID REFERENCES users(id) ON DELETE SET NULL;

UPDATE workspaces w
SET owner_id = wm.user_id
FROM workspace_members wm
WHERE wm.workspace_id = w.id AND wm.role = 'owner';

CREATE INDEX idx_workspaces_owner ON workspaces(owner_id) WHERE owner_id IS NOT NULL;
