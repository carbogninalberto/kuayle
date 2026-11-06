-- A label with project_id NULL is a workspace-wide label; otherwise it can only
-- be used on issues of that project.
ALTER TABLE labels ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_labels_project ON labels(project_id) WHERE project_id IS NOT NULL;

-- A label name is unique, ignoring case, within its scope: the workspace-wide
-- labels or one project's labels. Deleted labels free their name. Creates used
-- to check uniqueness without a constraint, so two racing creates could leave
-- active duplicates. Keep the oldest label's name and rename the others with a
-- short id suffix (they stay on their issues), so the index can be built.
-- Rolling back keeps the new names.
LOCK TABLE labels IN SHARE ROW EXCLUSIVE MODE;
WITH ranked AS (
    SELECT id, row_number() OVER (
        PARTITION BY workspace_id, project_id, lower(name) ORDER BY created_at, id
    ) AS position
    FROM labels
    WHERE deleted_at IS NULL
)
UPDATE labels SET name = left(labels.name, 39) || ' (' || left(labels.id::text, 8) || ')', updated_at = NOW()
FROM ranked
WHERE ranked.id = labels.id AND ranked.position > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_labels_scope_name ON labels (
    workspace_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name)
) WHERE deleted_at IS NULL;
