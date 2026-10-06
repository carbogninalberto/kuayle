-- Existing teams retain their workspace-public behavior.
ALTER TABLE teams ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_team_members_user_team ON team_members (user_id, team_id);
CREATE INDEX idx_teams_private_workspace ON teams (workspace_id) WHERE is_private;
