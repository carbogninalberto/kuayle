CREATE INDEX IF NOT EXISTS idx_issues_workspace_status_id ON issues(workspace_id, status_id);
CREATE INDEX IF NOT EXISTS idx_issues_workspace_team_updated ON issues(workspace_id, team_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_issues_workspace_created ON issues(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_issues_workspace_priority ON issues(workspace_id, priority);
CREATE INDEX IF NOT EXISTS idx_issues_workspace_due_date ON issues(workspace_id, due_date) WHERE due_date IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_issue_assignees_issue ON issue_assignees(issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_assignees_user_issue ON issue_assignees(user_id, issue_id);
