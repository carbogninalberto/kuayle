-- A team's key can be renamed. Issue identifiers (ENG-12) are stored on the
-- issues, so a rename rewrites them, and the old key is kept here forever so
-- old identifiers, links and branch names keep resolving. A key that is an
-- alias cannot be used by another team in the workspace.
CREATE TABLE team_key_aliases (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    old_key VARCHAR(10) NOT NULL,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, old_key)
);
CREATE INDEX team_key_aliases_team_id_idx ON team_key_aliases(team_id);
