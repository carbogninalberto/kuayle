CREATE TABLE IF NOT EXISTS issue_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(200) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS issue_group_items (
    group_id UUID NOT NULL REFERENCES issue_groups(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    PRIMARY KEY (group_id, issue_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_groups_workspace ON issue_groups(workspace_id);
CREATE INDEX IF NOT EXISTS idx_issue_group_items_issue ON issue_group_items(issue_id);
