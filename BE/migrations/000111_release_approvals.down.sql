-- Release approvals cannot exist under the older constraints, so they are
-- removed; the releases already published on GitHub are not affected.
DELETE FROM conversation_approvals WHERE action = 'create_release';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action = 'upgrade_instance');
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready'));
