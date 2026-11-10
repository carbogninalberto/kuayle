-- message_task asks for approval: text the assistant wants to send to a
-- task's coding agent reaches it only after the task's creator approved it.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task'));
-- The approval's request ID, so an approval that is repeated or resumed
-- delivers its message once. Messages typed by the task's owner have none.
ALTER TABLE agent_live_messages ADD COLUMN request_id uuid;
ALTER TABLE agent_live_messages ADD CONSTRAINT agent_live_messages_request_check CHECK (request_id IS NULL OR (direction = 'to_agent' AND sender = 'orchestrator'));
CREATE UNIQUE INDEX agent_live_messages_request ON agent_live_messages(task_id, request_id) WHERE request_id IS NOT NULL;
