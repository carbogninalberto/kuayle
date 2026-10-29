-- Preview and port approvals are discarded; previews and ports they started stay.
DELETE FROM conversation_approvals WHERE action IN ('start_preview', 'expose_port');
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work'));
