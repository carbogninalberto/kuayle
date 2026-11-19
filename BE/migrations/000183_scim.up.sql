ALTER TABLE users ADD COLUMN session_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE refresh_tokens ADD COLUMN session_version BIGINT NOT NULL DEFAULT 0;

CREATE TABLE workspace_scim_configs (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    token_hash TEXT,
    rotated_at TIMESTAMPTZ
);

CREATE TABLE scim_users (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    profile JSONB NOT NULL,
    active BOOLEAN NOT NULL,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE UNIQUE INDEX scim_users_username ON scim_users (workspace_id, lower(profile->>'userName')) WHERE NOT deleted;

CREATE TABLE scim_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    display_name TEXT NOT NULL,
    team_id UUID UNIQUE REFERENCES teams(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, id)
);
CREATE UNIQUE INDEX scim_groups_display_name ON scim_groups (workspace_id, lower(display_name));

CREATE TABLE scim_group_members (
    workspace_id UUID NOT NULL,
    group_id UUID NOT NULL,
    user_id UUID NOT NULL,
    PRIMARY KEY (workspace_id, group_id, user_id),
    FOREIGN KEY (workspace_id, group_id) REFERENCES scim_groups(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, user_id) REFERENCES scim_users(workspace_id, user_id) ON DELETE CASCADE
);
CREATE INDEX scim_group_members_user ON scim_group_members(workspace_id, user_id);

-- Track grants created by SCIM so unmapping never removes manual memberships.
CREATE TABLE scim_team_grants (
    team_id UUID NOT NULL,
    user_id UUID NOT NULL,
    PRIMARY KEY (team_id, user_id),
    FOREIGN KEY (team_id, user_id) REFERENCES team_members(team_id, user_id) ON DELETE CASCADE
);
