-- Wiki settings belong to one team or project wiki. The registry, a daily log
-- of what happened in the scope that agents and the assistant append to, is
-- opt-in: no row means it is off. Access follows the owning scope through the
-- repository's wiki visibility predicates.
CREATE TABLE wiki_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    registry_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wiki_settings_single_scope CHECK ((team_id IS NULL) <> (project_id IS NULL))
);

CREATE UNIQUE INDEX wiki_settings_team_key ON wiki_settings (team_id) WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX wiki_settings_project_key ON wiki_settings (project_id) WHERE project_id IS NOT NULL;

-- A registry page holds one UTC day of its scope's registry.
ALTER TABLE wiki_pages ADD COLUMN registry_date DATE;

CREATE UNIQUE INDEX wiki_pages_team_registry_key ON wiki_pages (team_id, registry_date) WHERE team_id IS NOT NULL AND registry_date IS NOT NULL;
CREATE UNIQUE INDEX wiki_pages_project_registry_key ON wiki_pages (project_id, registry_date) WHERE project_id IS NOT NULL AND registry_date IS NOT NULL;
