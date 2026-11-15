-- Environment status: what the machine manager last found in a running
-- machine's developer container (today only whether apt's package index is
-- present) and the state of the last on-demand index refresh.
-- A row describes one container, never the machine: it is deleted when the
-- machine stops, pauses or is torn down, and a row naming another container
-- is stale. It never feeds the machine or service health.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'refresh_package_index';

CREATE TABLE dev_machine_environment_checks (
    machine_id uuid PRIMARY KEY REFERENCES dev_machines(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    container_id text NOT NULL,
    package_index_state text NOT NULL
        CHECK (package_index_state IN ('absent','present','unknown')),
    package_index_mtime timestamptz NULL,
    checked_at timestamptz NOT NULL,
    refresh_state text NOT NULL DEFAULT 'idle'
        CHECK (refresh_state IN ('idle','queued','running','succeeded','failed')),
    refresh_exit_code integer NULL,
    refresh_output text NULL CHECK (refresh_output IS NULL OR octet_length(refresh_output) <= 8192),
    refresh_started_at timestamptz NULL,
    refresh_finished_at timestamptz NULL
);
