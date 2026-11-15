-- PostgreSQL cannot remove an enum value in place; the inert 'run_command'
-- value stays. Queued runs cannot execute on the older manager, so their
-- operations are dropped with the table. Approvals of the new actions cannot
-- exist under the older constraints and are removed; temporary machines become
-- ordinary machines that their owners delete.
DELETE FROM dev_machine_operations WHERE action = 'run_command';
DROP TABLE IF EXISTS assistant_command_runs;
DELETE FROM conversation_approvals WHERE action IN ('start_temporary_machine', 'run_command', 'run_command_write');
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task'));
ALTER TABLE dev_machine_workspace_policies DROP COLUMN IF EXISTS max_temporary_machines;
DROP INDEX IF EXISTS dev_machines_temporary_expiry;
ALTER TABLE dev_machines DROP COLUMN IF EXISTS temporary_expires_at, DROP COLUMN IF EXISTS temporary_conversation_id;
