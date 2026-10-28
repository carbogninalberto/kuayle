-- A user's free chats can be tagged with a project of the same workspace. The
-- tag only filters the owner's own list; conversations stay private. Issue
-- chats follow their issue's current project, so they never store one.
ALTER TABLE conversations
 ADD COLUMN project_id uuid,
 ADD CONSTRAINT conversations_project_fk FOREIGN KEY (workspace_id, project_id)
  REFERENCES projects(workspace_id, id) ON DELETE SET NULL (project_id),
 ADD CONSTRAINT conversations_project_free_chat CHECK (issue_id IS NULL OR project_id IS NULL);
CREATE INDEX conversations_owner_project ON conversations(workspace_id, creator_id, project_id) WHERE project_id IS NOT NULL;
