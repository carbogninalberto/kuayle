CREATE TABLE IF NOT EXISTS issue_assignees (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (issue_id, user_id)
);

-- Migrate existing single assignee data
INSERT INTO issue_assignees (issue_id, user_id)
SELECT id, assignee_id FROM issues WHERE assignee_id IS NOT NULL
ON CONFLICT DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_issue_assignees_user ON issue_assignees(user_id);
