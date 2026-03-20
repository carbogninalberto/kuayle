-- Project status visibility junction table
CREATE TABLE IF NOT EXISTS project_status_visibility (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status_id  UUID NOT NULL REFERENCES team_statuses(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, status_id)
);

CREATE INDEX IF NOT EXISTS idx_project_status_visibility_project ON project_status_visibility(project_id);

-- Ensure every team has the 6 default statuses (idempotent)
INSERT INTO team_statuses (id, team_id, name, slug, category, position, is_default)
SELECT gen_random_uuid(), t.id, s.name, s.slug, s.category, s.pos, true
FROM teams t
CROSS JOIN (VALUES
    ('Backlog',     'backlog',     'backlog',   0),
    ('Todo',        'todo',        'unstarted', 1),
    ('In Progress', 'in_progress', 'started',   2),
    ('In Review',   'in_review',   'started',   3),
    ('Done',        'done',        'completed', 4),
    ('Cancelled',   'cancelled',   'cancelled', 5)
) AS s(name, slug, category, pos)
WHERE NOT EXISTS (
    SELECT 1 FROM team_statuses ts
    WHERE ts.team_id = t.id AND ts.slug = s.slug
);

-- Populate status_id from matching team_statuses slug
UPDATE issues i SET status_id = (
    SELECT ts.id FROM team_statuses ts
    WHERE ts.team_id = i.team_id AND ts.slug = i.status
    LIMIT 1
) WHERE i.status_id IS NULL;

-- For any remaining NULLs, fall back to the team's default backlog status
UPDATE issues i SET status_id = (
    SELECT ts.id FROM team_statuses ts
    WHERE ts.team_id = i.team_id AND ts.is_default = true
    ORDER BY ts.position
    LIMIT 1
) WHERE i.status_id IS NULL;

-- Now make status_id NOT NULL
ALTER TABLE issues ALTER COLUMN status_id SET NOT NULL;
