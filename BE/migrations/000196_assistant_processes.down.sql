-- Stop live processes before rolling back. PostgreSQL retains the unused enum value.
DELETE FROM dev_machine_operations WHERE action='start_process';
DELETE FROM dev_machine_ports WHERE source='process';
DROP FUNCTION IF EXISTS kuayle_assistant_process_available(UUID, BOOLEAN);
DROP TABLE assistant_processes;
ALTER TABLE dev_machine_ports DROP CONSTRAINT dev_machine_ports_source_check,
 ADD CONSTRAINT dev_machine_ports_source_check CHECK (source IN ('user','agent','task'));
ALTER TABLE dev_machine_ports DROP CONSTRAINT dev_machine_ports_process_target_check,
 DROP CONSTRAINT dev_machine_ports_target_check,
 ADD CONSTRAINT dev_machine_ports_target_check CHECK (target IN ('developer','docker','preview'));
DELETE FROM conversation_approvals WHERE action='start_process';

ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues', 'run_migration', 'publish_task_changes'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues', 'run_migration'));
