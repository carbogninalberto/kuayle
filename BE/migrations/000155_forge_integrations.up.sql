-- Forge integrations (GitLab, Gitea, Forgejo). GitHub keeps its own tables so
-- its existing data and behaviour stay untouched; activity responses merge
-- both sources. A connection is one admin-configured forge instance with its
-- own webhook secret and optional access token.
CREATE TABLE forge_connections (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider            VARCHAR(20) NOT NULL CHECK (provider IN ('gitlab', 'gitea', 'forgejo')),
    name                VARCHAR(255) NOT NULL DEFAULT '',
    base_url            TEXT NOT NULL,
    webhook_secret      TEXT NOT NULL,
    access_token        TEXT,
    created_by          UUID NOT NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(workspace_id, provider, base_url)
);

CREATE INDEX idx_forge_connections_workspace ON forge_connections(workspace_id);

-- Repositories linked to a connection. Webhooks for any other repository are
-- acknowledged and ignored.
CREATE TABLE forge_repos (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id       UUID NOT NULL REFERENCES forge_connections(id) ON DELETE CASCADE,
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    external_repo_id    BIGINT NOT NULL,
    full_name           VARCHAR(512) NOT NULL,
    web_url             TEXT NOT NULL DEFAULT '',
    default_branch      VARCHAR(255) NOT NULL DEFAULT 'main',
    is_active           BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(connection_id, external_repo_id)
);

CREATE INDEX idx_forge_repos_workspace ON forge_repos(workspace_id);

-- Merge/pull requests linked to issues.
CREATE TABLE forge_merge_requests (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,
    forge_repo_id       UUID NOT NULL REFERENCES forge_repos(id) ON DELETE CASCADE,
    external_id         BIGINT NOT NULL,
    number              INT NOT NULL,
    title               VARCHAR(1024) NOT NULL,
    state               VARCHAR(20) NOT NULL CHECK (state IN ('open', 'closed', 'merged', 'draft')),
    author_login        VARCHAR(255) NOT NULL,
    author_avatar_url   TEXT,
    web_url             TEXT NOT NULL,
    source_branch       VARCHAR(512),
    target_branch       VARCHAR(512),
    additions           INT NOT NULL DEFAULT 0,
    deletions           INT NOT NULL DEFAULT 0,
    merged_at           TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(forge_repo_id, external_id)
);

CREATE INDEX idx_forge_mrs_issue ON forge_merge_requests(issue_id);
CREATE INDEX idx_forge_mrs_workspace ON forge_merge_requests(workspace_id);

-- Branches linked to issues.
CREATE TABLE forge_branches (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,
    forge_repo_id       UUID NOT NULL REFERENCES forge_repos(id) ON DELETE CASCADE,
    name                VARCHAR(512) NOT NULL,
    web_url             TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(forge_repo_id, name)
);

CREATE INDEX idx_forge_branches_issue ON forge_branches(issue_id);
CREATE INDEX idx_forge_branches_workspace ON forge_branches(workspace_id);

-- Commits linked to issues. SHA-256 is 64 hex characters.
CREATE TABLE forge_commits (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,
    forge_repo_id       UUID NOT NULL REFERENCES forge_repos(id) ON DELETE CASCADE,
    sha                 VARCHAR(64) NOT NULL,
    message             TEXT NOT NULL,
    author_login        VARCHAR(255),
    author_avatar_url   TEXT,
    web_url             TEXT NOT NULL,
    committed_at        TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(forge_repo_id, sha)
);

CREATE INDEX idx_forge_commits_issue ON forge_commits(issue_id);
CREATE INDEX idx_forge_commits_workspace ON forge_commits(workspace_id);

-- One row per webhook delivery the server accepted, so a captured, validly
-- signed delivery cannot be processed twice. There is no foreign key on
-- purpose, like github_webhook_deliveries: the marker must outlive a deleted
-- connection, or a replayed delivery could act again after reconnecting.
-- processed_at stays NULL while handling; a stale claim may be taken over.
CREATE TABLE forge_webhook_deliveries (
    connection_id       UUID NOT NULL,
    delivery_id         TEXT NOT NULL CHECK (char_length(delivery_id) BETWEEN 1 AND 128),
    claimed_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at        TIMESTAMPTZ,
    PRIMARY KEY (connection_id, delivery_id)
);

CREATE INDEX idx_forge_webhook_deliveries_claimed_at ON forge_webhook_deliveries(claimed_at);
