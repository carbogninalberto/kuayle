-- The assistant's pull request tools ask for approval before replying to a
-- review comment, resolving a thread, updating the branch or changing the
-- draft state of a pull request.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready'));

-- How merge_pull_request merges the pull requests of a repository, set in the
-- development settings. A repository without a row uses the first method it
-- allows (squash, merge, rebase).
CREATE TABLE repository_merge_methods (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    repository_full_name TEXT NOT NULL,
    merge_method TEXT NOT NULL CHECK (merge_method IN ('merge', 'squash', 'rebase')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, repository_full_name)
);
