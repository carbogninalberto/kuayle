CREATE TABLE shared_links (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token               VARCHAR(64) NOT NULL UNIQUE,
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    created_by          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope               VARCHAR(20) NOT NULL CHECK (scope IN ('workspace', 'team', 'project', 'view')),
    scope_id            UUID,
    filters             JSONB NOT NULL DEFAULT '{}',
    include_description BOOLEAN NOT NULL DEFAULT false,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_shared_links_token ON shared_links(token);
CREATE INDEX idx_shared_links_workspace ON shared_links(workspace_id);

ALTER TABLE workspaces ADD COLUMN share_link_min_role VARCHAR(20) NOT NULL DEFAULT 'admin'
    CHECK (share_link_min_role IN ('owner', 'admin', 'member'));
