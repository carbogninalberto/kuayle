-- An issue conversation retains its original team boundary. Moving or deleting
-- the issue must never turn its historical content into an unscoped chat.
ALTER TABLE issues ADD CONSTRAINT issues_conversation_scope UNIQUE(id,workspace_id);
ALTER TABLE conversations
 ADD COLUMN issue_id uuid,
 ADD COLUMN team_id uuid REFERENCES teams(id) ON DELETE CASCADE,
 ADD CONSTRAINT conversation_issue_scope FOREIGN KEY(issue_id,workspace_id)
  REFERENCES issues(id,workspace_id) ON DELETE CASCADE,
 ADD CONSTRAINT conversation_scope_pair CHECK ((issue_id IS NULL) = (team_id IS NULL));
CREATE UNIQUE INDEX conversation_issue_owner ON conversations(workspace_id,creator_id,issue_id,team_id) WHERE issue_id IS NOT NULL;
