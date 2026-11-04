-- A label with project_id NULL is a workspace-wide label; otherwise it can only
-- be used on issues of that project.
ALTER TABLE labels ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_labels_project ON labels(project_id) WHERE project_id IS NOT NULL;
