-- The head commit GitHub last reported for the pull request of an agent task,
-- from pull_request webhooks and fresh reads before a merge. It can differ from
-- the commit Kuayle published when someone else pushes to the task branch.
-- Review results at another commit are stale.
CREATE TABLE pull_request_heads (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    repository_full_name TEXT NOT NULL,
    pull_request_number INT NOT NULL CHECK (pull_request_number > 0),
    head_sha TEXT NOT NULL CHECK (head_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    -- When GitHub (or a fresh read) observed this head; an older observation never replaces a newer one.
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, repository_full_name, pull_request_number)
);
