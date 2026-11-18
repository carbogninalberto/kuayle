-- Rolling back removes the named migration actions and the migration mark of
-- command runs; ordinary command runs and their approvals stay. Approvals of
-- run_migration (no pending side effects) are deleted so the restored action
-- list holds.
DELETE FROM conversation_approvals WHERE action = 'run_migration';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues'));

ALTER TABLE assistant_command_runs DROP CONSTRAINT assistant_command_runs_migration_check;
ALTER TABLE assistant_command_runs
    DROP COLUMN action_name,
    DROP COLUMN migration;

DROP TABLE dev_machine_migration_actions;
