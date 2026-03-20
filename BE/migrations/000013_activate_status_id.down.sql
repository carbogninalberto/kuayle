ALTER TABLE issues ALTER COLUMN status_id DROP NOT NULL;
DROP TABLE IF EXISTS project_status_visibility;
