-- Runs of assistant custom tools. A run_tool machine operation executes one
-- tool version in the credential-free check container of a dev machine; its
-- idempotency key names the run. The run keeps the validated inputs, who asked,
-- where it ran and its result with the tail of its output.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'run_tool';

CREATE TABLE assistant_tool_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    tool_id UUID NOT NULL,
    tool_version INT NOT NULL CHECK (tool_version >= 1),
    tool_name TEXT NOT NULL,
    inputs JSONB NOT NULL CHECK (jsonb_typeof(inputs) = 'object' AND octet_length(inputs::text) <= 16384),
    requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    tool_call_id UUID REFERENCES conversation_tool_calls(id) ON DELETE SET NULL,
    -- An approved run is created once per approval, however often it is approved.
    approval_id UUID UNIQUE REFERENCES conversation_approvals(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES dev_machines(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
    -- The task attempt whose checkout the run uses and whose redaction
    -- snapshot hides credentials from the output.
    agent_run_id UUID REFERENCES dev_machine_agent_runs(id) ON DELETE SET NULL,
    working_directory TEXT NOT NULL CHECK (working_directory LIKE '/workspace%'),
    timeout_seconds INT NOT NULL CHECK (timeout_seconds BETWEEN 10 AND 1800),
    test BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    exit_code INT,
    output TEXT NOT NULL DEFAULT '' CHECK (octet_length(output) <= 262144),
    output_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT CHECK (octet_length(error) <= 2048),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    CONSTRAINT fk_assistant_tool_runs_tool FOREIGN KEY (workspace_id, tool_id) REFERENCES assistant_custom_tools(workspace_id, id) ON DELETE CASCADE,
    CHECK ((status IN ('queued', 'running')) = (finished_at IS NULL))
);
CREATE INDEX assistant_tool_runs_tool ON assistant_tool_runs (tool_id, created_at DESC);
CREATE INDEX assistant_tool_runs_machine ON assistant_tool_runs (machine_id) WHERE status IN ('queued', 'running');

-- The model calls a custom tool as custom_<name>; names may contain digits.
ALTER TABLE conversation_tool_calls DROP CONSTRAINT conversation_tool_calls_name_check;
ALTER TABLE conversation_tool_calls ADD CONSTRAINT conversation_tool_calls_name_check CHECK (name ~ '^[a-z_][a-z0-9_]{0,63}$');

-- A custom tool in approval mode asks for approval before every run.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool'));
