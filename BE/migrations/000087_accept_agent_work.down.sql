-- Accept approvals are discarded; tasks they completed stay completed.
DELETE FROM conversation_approvals WHERE action='accept_agent_work';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task'));
