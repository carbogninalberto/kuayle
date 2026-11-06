DROP INDEX IF EXISTS idx_labels_project;
ALTER TABLE labels DROP COLUMN IF EXISTS project_id;
