-- A workspace can edit the built-in Standard workflow: its edits are stored as
-- a workflow marked builtin_key 'standard', at most one per workspace, which
-- tasks then run wherever Standard is chosen or inherited. It is not listed
-- among the workspace's own workflows, so its name never clashes with theirs.
ALTER TABLE workflows ADD COLUMN builtin_key TEXT CHECK (builtin_key IN ('standard'));
CREATE UNIQUE INDEX workflows_builtin ON workflows (workspace_id, builtin_key) WHERE builtin_key IS NOT NULL;
DROP INDEX workflows_name;
CREATE UNIQUE INDEX workflows_name ON workflows (workspace_id, lower(name)) WHERE archived_at IS NULL AND builtin_key IS NULL;
