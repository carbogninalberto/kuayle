-- Short commands the assistant runs on a Dev Machine (run_command and
-- run_command_write), and the small temporary machines it can start for them.
-- A run executes once, after approval, in a credential-free check container;
-- its run_command operation names the run in its idempotency key.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'run_command';

-- A temporary machine belongs to the conversation that started it and is
-- permanently deleted once temporary_expires_at passes.
ALTER TABLE dev_machines
    ADD COLUMN temporary_conversation_id UUID NULL REFERENCES conversations(id) ON DELETE SET NULL,
    ADD COLUMN temporary_expires_at TIMESTAMPTZ NULL;
CREATE INDEX dev_machines_temporary_expiry ON dev_machines (temporary_expires_at)
    WHERE temporary_expires_at IS NOT NULL;

-- How many temporary machines may be live in the workspace at once; 0 turns
-- them off. Each user also has at most one.
ALTER TABLE dev_machine_workspace_policies
    ADD COLUMN max_temporary_machines INT NOT NULL DEFAULT 2
    CHECK (max_temporary_machines BETWEEN 0 AND 10);

CREATE TABLE assistant_command_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID REFERENCES dev_machines(id) ON DELETE SET NULL,
    -- Kept when the machine is purged, so the transcript still names it.
    machine_name TEXT NOT NULL,
    cpu_millis INT NOT NULL,
    memory_mb INT NOT NULL,
    disk_gb INT NOT NULL,
    temporary BOOLEAN NOT NULL DEFAULT FALSE,
    reused BOOLEAN NOT NULL DEFAULT FALSE,
    requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    tool_call_id UUID REFERENCES conversation_tool_calls(id) ON DELETE SET NULL,
    -- An approval runs its command at most once.
    approval_id UUID UNIQUE REFERENCES conversation_approvals(id) ON DELETE SET NULL,
    command TEXT NOT NULL CHECK (octet_length(command) BETWEEN 1 AND 4000),
    write BOOLEAN NOT NULL DEFAULT FALSE,
    working_directory TEXT NOT NULL CHECK (working_directory LIKE '/workspace%' AND working_directory NOT LIKE '%..%'),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    exit_code INT,
    output TEXT NOT NULL DEFAULT '' CHECK (octet_length(output) <= 65536),
    output_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT CHECK (octet_length(error) <= 2048),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    CHECK ((status IN ('queued', 'running')) = (finished_at IS NULL))
);
CREATE INDEX assistant_command_runs_conversation ON assistant_command_runs (conversation_id, created_at DESC);
CREATE INDEX assistant_command_runs_machine ON assistant_command_runs (machine_id) WHERE status IN ('queued', 'running');

-- Starting a temporary machine and running a command ask for approval; none
-- of them belongs to an issue.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'start_temporary_machine', 'run_command', 'run_command_write'));
