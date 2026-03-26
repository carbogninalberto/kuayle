-- GitHub App installations (one per workspace)
CREATE TABLE github_installations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
    installation_id     BIGINT NOT NULL UNIQUE,
    account_login       VARCHAR(255) NOT NULL,
    account_type        VARCHAR(20) NOT NULL CHECK (account_type IN ('Organization','User')),
    access_token        TEXT,
    token_expires_at    TIMESTAMPTZ,
    installed_by        UUID NOT NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Linked repositories
CREATE TABLE github_repos (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id     UUID NOT NULL REFERENCES github_installations(id) ON DELETE CASCADE,
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    github_repo_id      BIGINT NOT NULL,
    full_name           VARCHAR(512) NOT NULL,
    default_branch      VARCHAR(255) NOT NULL DEFAULT 'main',
    is_active           BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(workspace_id, github_repo_id)
);

CREATE INDEX idx_github_repos_workspace ON github_repos(workspace_id);

-- Pull requests linked to issues
CREATE TABLE github_pull_requests (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,
    github_repo_id      UUID NOT NULL REFERENCES github_repos(id) ON DELETE CASCADE,
    github_pr_id        BIGINT NOT NULL,
    number              INT NOT NULL,
    title               VARCHAR(1024) NOT NULL,
    state               VARCHAR(20) NOT NULL CHECK (state IN ('open','closed','merged','draft')),
    author_login        VARCHAR(255) NOT NULL,
    author_avatar_url   TEXT,
    html_url            TEXT NOT NULL,
    head_branch         VARCHAR(512),
    base_branch         VARCHAR(512),
    additions           INT NOT NULL DEFAULT 0,
    deletions           INT NOT NULL DEFAULT 0,
    merged_at           TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(github_repo_id, github_pr_id)
);

CREATE INDEX idx_github_prs_issue ON github_pull_requests(issue_id);
CREATE INDEX idx_github_prs_workspace ON github_pull_requests(workspace_id);

-- Branches linked to issues
CREATE TABLE github_branches (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,
    github_repo_id      UUID NOT NULL REFERENCES github_repos(id) ON DELETE CASCADE,
    name                VARCHAR(512) NOT NULL,
    html_url            TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(github_repo_id, name)
);

CREATE INDEX idx_github_branches_issue ON github_branches(issue_id);

-- Commits linked to issues
CREATE TABLE github_commits (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,
    github_repo_id      UUID NOT NULL REFERENCES github_repos(id) ON DELETE CASCADE,
    pr_id               UUID REFERENCES github_pull_requests(id) ON DELETE SET NULL,
    sha                 VARCHAR(40) NOT NULL,
    message             TEXT NOT NULL,
    author_login        VARCHAR(255),
    author_avatar_url   TEXT,
    html_url            TEXT NOT NULL,
    committed_at        TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(github_repo_id, sha)
);

CREATE INDEX idx_github_commits_issue ON github_commits(issue_id);
CREATE INDEX idx_github_commits_pr ON github_commits(pr_id);

-- Auto-transition rules per workspace
CREATE TABLE github_auto_transitions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    event               VARCHAR(50) NOT NULL,
    target_status       VARCHAR(50) NOT NULL,
    target_status_id    UUID REFERENCES team_statuses(id) ON DELETE SET NULL,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    UNIQUE(workspace_id, event)
);
