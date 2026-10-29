-- Workspace skills for the assistant: Markdown guidance, optionally scoped to a
-- team, that the assistant lists by name and description and loads on demand.
-- The skill row holds the content in effect (its current_version); every saved
-- edit is a row in assistant_skill_versions. A draft version above
-- current_version is a proposed edit an owner or admin has not activated yet;
-- the assistant only ever writes drafts.
CREATE TABLE assistant_skills (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    body TEXT NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 65536),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL CHECK (status IN ('draft', 'active')),
    current_version INT NOT NULL CHECK (current_version >= 1),
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    author_kind TEXT NOT NULL CHECK (author_kind IN ('user', 'assistant')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Names are unique, ignoring case, among the workspace-wide skills and among
-- each team's skills.
CREATE UNIQUE INDEX assistant_skills_name ON assistant_skills (workspace_id, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
CREATE INDEX assistant_skills_team ON assistant_skills (team_id) WHERE team_id IS NOT NULL;

CREATE TABLE assistant_skill_versions (
    skill_id UUID NOT NULL REFERENCES assistant_skills(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version >= 1),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    body TEXT NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 65536),
    -- draft: proposed and not activated yet; active: has been put in effect.
    status TEXT NOT NULL CHECK (status IN ('draft', 'active')),
    author_kind TEXT NOT NULL CHECK (author_kind IN ('user', 'assistant')),
    author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (skill_id, version)
);
