-- message_task approvals cannot exist under the older constraint, so they are
-- removed; messages they already delivered are kept without their request ID.
DROP INDEX agent_live_messages_request;
ALTER TABLE agent_live_messages DROP CONSTRAINT agent_live_messages_request_check;
ALTER TABLE agent_live_messages DROP COLUMN request_id;
DELETE FROM conversation_approvals WHERE action = 'message_task';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page'));
