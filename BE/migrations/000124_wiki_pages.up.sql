-- Wiki pages belong to exactly one team or one project. Access follows the
-- owning scope (team visibility, or the project's workspace/team visibility)
-- and is enforced by repository predicates; workspace_id pins the page to the
-- workspace that owns the scope. Deleting a team, project or workspace removes
-- its pages.
CREATE TABLE wiki_pages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    title VARCHAR(200) NOT NULL CHECK (btrim(title) <> ''),
    content TEXT NOT NULL DEFAULT '' CHECK (octet_length(content) <= 524288),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wiki_pages_single_scope CHECK ((team_id IS NULL) <> (project_id IS NULL))
);

CREATE INDEX wiki_pages_team_idx ON wiki_pages (team_id, updated_at DESC) WHERE team_id IS NOT NULL;
CREATE INDEX wiki_pages_project_idx ON wiki_pages (project_id, updated_at DESC) WHERE project_id IS NOT NULL;
