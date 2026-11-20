-- A process uses its approval ID for idempotency and its app port's ID.
-- Keep the process row after its conversation is deleted so the manager can
-- remove the container. A purged machine has already lost all its containers.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'start_process';
CREATE TABLE assistant_processes (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES dev_machines(id) ON DELETE SET NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    generation BIGINT NOT NULL,
    routing_key TEXT NOT NULL,
    command TEXT NOT NULL CHECK (octet_length(command) BETWEEN 1 AND 4000),
    working_directory TEXT NOT NULL,
    port INT NOT NULL CHECK (port BETWEEN 1024 AND 65535),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'stopping', 'stopped', 'failed')),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX assistant_processes_conversation ON assistant_processes (conversation_id);
CREATE INDEX assistant_processes_active ON assistant_processes (workspace_id, machine_id, user_id)
 WHERE status IN ('queued', 'running', 'stopping');

ALTER TABLE dev_machine_ports DROP CONSTRAINT dev_machine_ports_source_check,
 ADD CONSTRAINT dev_machine_ports_source_check CHECK (source IN ('user','agent','task','process'));
ALTER TABLE dev_machine_ports DROP CONSTRAINT dev_machine_ports_target_check,
 ADD CONSTRAINT dev_machine_ports_target_check CHECK (target IN ('developer','docker','preview','process')),
 ADD CONSTRAINT dev_machine_ports_process_target_check CHECK ((source='process')=(target='process'));

-- The gateway can test an app-port ID without reading process commands or chats.
CREATE FUNCTION kuayle_assistant_process_available(process_id UUID, require_running BOOLEAN)
RETURNS BOOLEAN LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp AS $$
 SELECT EXISTS(
   SELECT 1 FROM assistant_processes p
   JOIN conversations c ON c.id=p.conversation_id AND c.workspace_id=p.workspace_id AND c.creator_id=p.user_id
   JOIN workspace_members wm ON wm.workspace_id=p.workspace_id AND wm.user_id=p.user_id
   JOIN dev_machines m ON m.id=p.machine_id AND m.workspace_id=p.workspace_id AND m.created_by_user_id=p.user_id
   JOIN dev_machine_workspace_policies policy ON policy.workspace_id=p.workspace_id AND policy.enabled
   JOIN dev_machine_ports port ON port.id=p.id AND port.machine_id=p.machine_id AND port.workspace_id=p.workspace_id
      AND port.port=p.port AND port.source='process'
   WHERE p.id=process_id AND (NOT require_running OR p.status='running')
   AND p.expires_at>now() AND wm.role IN ('owner','admin','member')
   AND m.generation=p.generation AND m.status='running' AND m.desired_status='running' AND m.delete_requested_at IS NULL
   AND (c.issue_id IS NULL OR EXISTS(SELECT 1 FROM issues i WHERE i.id=c.issue_id AND i.workspace_id=c.workspace_id
     AND i.team_id=c.team_id AND kuayle_team_access(i.team_id,c.creator_id,c.workspace_id)))
 )
$$;
REVOKE ALL ON FUNCTION kuayle_assistant_process_available(UUID, BOOLEAN) FROM PUBLIC;

ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues', 'run_migration', 'publish_task_changes', 'start_process'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'start_temporary_machine', 'run_command', 'run_command_write', 'bulk_update_issues', 'run_migration', 'start_process'));
