-- Rolling back removes batch approvals and their results. The tool history remains.
DELETE FROM conversation_approvals WHERE action = 'bulk_update_issues';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'start_temporary_machine', 'run_command', 'run_command_write'));

ALTER TABLE conversation_tool_calls DROP CONSTRAINT conversation_tool_calls_check;
ALTER TABLE conversation_tool_calls ADD CONSTRAINT conversation_tool_calls_check CHECK (
    octet_length(input::text) <= 16384 AND (output IS NULL OR octet_length(output::text) <= 32768)) NOT VALID;
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_payload_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_payload_check CHECK (octet_length(payload::text) <= 40960);
