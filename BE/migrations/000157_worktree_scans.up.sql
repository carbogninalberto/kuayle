-- Read-only worktree scans run rg and git in a task checkout on a running Dev
-- Machine. The scan row and its machine operation share one ID, so the manager
-- stores the result on the row it leased. Scans are read-only and may run beside
-- an agent attempt, a validation, a check or another scan.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'scan_worktree';

CREATE TABLE dev_machine_worktree_scans (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    -- The task's checkout, and the task that asked, when the scan reads one.
    -- They are kept without a foreign key so the scan record survives a task or
    -- checkout cleanup.
    checkout_id UUID,
    task_id UUID,
    requested_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request JSONB NOT NULL CHECK (jsonb_typeof(request) = 'object'),
    result JSONB,
    error TEXT CHECK (error IS NULL OR octet_length(error) <= 2048),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CHECK (result IS NULL OR octet_length(result::text) <= 65536)
);
CREATE INDEX dev_machine_worktree_scans_workspace
    ON dev_machine_worktree_scans (workspace_id, created_at DESC);
