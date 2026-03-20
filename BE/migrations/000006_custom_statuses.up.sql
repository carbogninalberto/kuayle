-- Custom statuses per team
CREATE TABLE IF NOT EXISTS team_statuses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    category VARCHAR(50) NOT NULL CHECK (category IN ('backlog', 'unstarted', 'started', 'completed', 'cancelled')),
    color VARCHAR(20),
    position INT NOT NULL DEFAULT 0,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(team_id, slug)
);

-- Seed default statuses for all existing teams
INSERT INTO team_statuses (team_id, name, slug, category, position, is_default)
SELECT t.id, 'Backlog', 'backlog', 'backlog', 0, true FROM teams t
UNION ALL
SELECT t.id, 'Todo', 'todo', 'unstarted', 1, false FROM teams t
UNION ALL
SELECT t.id, 'In Progress', 'in_progress', 'started', 2, false FROM teams t
UNION ALL
SELECT t.id, 'In Review', 'in_review', 'started', 3, false FROM teams t
UNION ALL
SELECT t.id, 'Done', 'done', 'completed', 4, false FROM teams t
UNION ALL
SELECT t.id, 'Cancelled', 'cancelled', 'cancelled', 5, false FROM teams t
ON CONFLICT DO NOTHING;

-- Add status_id to issues (nullable initially for migration)
ALTER TABLE issues ADD COLUMN IF NOT EXISTS status_id UUID REFERENCES team_statuses(id);

-- Populate status_id from existing status text
UPDATE issues i
SET status_id = ts.id
FROM team_statuses ts
WHERE ts.team_id = i.team_id AND ts.slug = i.status::text;

CREATE INDEX IF NOT EXISTS idx_team_statuses_team ON team_statuses(team_id, position);
CREATE INDEX IF NOT EXISTS idx_issues_status_id ON issues(status_id);
