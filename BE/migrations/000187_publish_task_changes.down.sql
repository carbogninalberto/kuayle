-- Drop unpublished requests and their operations. Published branches and PRs remain on GitHub.
DELETE FROM dev_machine_operations WHERE action='publish_agent' AND idempotency_key LIKE 'stopped-publication:%';
DELETE FROM dev_machine_agent_run_steps s USING conversation_approvals a
WHERE a.action='publish_task_changes' AND s.id=a.request_id AND s.status IN ('queued','running');
DELETE FROM conversation_approvals WHERE action='publish_task_changes';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues', 'run_migration'));
