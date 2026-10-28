DROP INDEX IF EXISTS conversations_owner_project;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_project_free_chat,
 DROP CONSTRAINT IF EXISTS conversations_project_fk,
 DROP COLUMN IF EXISTS project_id;
