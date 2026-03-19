-- Phase 2A: Issue Relations
CREATE TABLE issue_relations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    related_issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    type VARCHAR(20) NOT NULL CHECK (type IN ('related', 'blocked_by', 'blocking', 'duplicate')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (issue_id, related_issue_id, type)
);

CREATE INDEX idx_issue_relations_issue ON issue_relations(issue_id);
CREATE INDEX idx_issue_relations_related ON issue_relations(related_issue_id);

-- Phase 2C: Estimate Scales
ALTER TABLE teams ADD COLUMN estimate_scale VARCHAR(20) NOT NULL DEFAULT 'linear';

-- Phase 2D: Issue Templates
CREATE TABLE issue_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id UUID REFERENCES teams(id) ON DELETE SET NULL,
    title VARCHAR(500) NOT NULL,
    description TEXT,
    status VARCHAR(20) DEFAULT 'backlog',
    priority INT DEFAULT 0,
    assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
    label_ids JSONB NOT NULL DEFAULT '[]',
    recurrence_rule JSONB,
    next_run_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_issue_templates_workspace ON issue_templates(workspace_id);
CREATE INDEX idx_issue_templates_next_run ON issue_templates(next_run_at) WHERE is_active = true AND next_run_at IS NOT NULL;
