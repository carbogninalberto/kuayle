-- Project labels become workspace-wide labels again. Labels renamed by the up
-- migration keep their new names.
DROP INDEX IF EXISTS idx_labels_scope_name;
DROP INDEX IF EXISTS idx_labels_project;
ALTER TABLE labels DROP COLUMN IF EXISTS project_id;
