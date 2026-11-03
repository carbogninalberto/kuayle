-- The assistant's upgrade_instance tool asks for approval to upgrade the whole
-- instance, which belongs to no issue, so an approval may now have none.
ALTER TABLE conversation_approvals ALTER COLUMN issue_id DROP NOT NULL;
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance'));
-- Only upgrades may lack an issue.
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action = 'upgrade_instance');
