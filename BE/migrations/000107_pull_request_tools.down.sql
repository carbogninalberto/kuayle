DROP TABLE repository_merge_methods;
DELETE FROM conversation_approvals WHERE action IN ('reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready');
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance'));
