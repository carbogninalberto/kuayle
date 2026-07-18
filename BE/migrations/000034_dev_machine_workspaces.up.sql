ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'checkout_issue';
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'snapshot_environment';

ALTER TABLE dev_machine_workspace_policies
    ADD COLUMN idle_pause_minutes INT NOT NULL DEFAULT 240
        CHECK (idle_pause_minutes BETWEEN 5 AND 10080);

ALTER TABLE dev_machines
    ALTER COLUMN repo_url SET DEFAULT '',
    ALTER COLUMN repo_owner SET DEFAULT '',
    ALTER COLUMN repo_name SET DEFAULT '',
    ALTER COLUMN working_branch SET DEFAULT '',
    ALTER COLUMN base_branch SET DEFAULT '';

CREATE TABLE dev_machine_environments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    image_ref TEXT NOT NULL,
    image_digest VARCHAR(255),
    status VARCHAR(32) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'building', 'ready', 'failed', 'delete_requested')),
    source_machine_id UUID REFERENCES dev_machines(id) ON DELETE SET NULL,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE dev_machine_scope_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issues(id) ON DELETE CASCADE,
    github_repo_id UUID REFERENCES github_repos(id) ON DELETE SET NULL,
    base_branch VARCHAR(255),
    environment_id UUID REFERENCES dev_machine_environments(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (num_nonnulls(team_id, project_id, issue_id) <= 1),
    CHECK (base_branch IS NULL OR base_branch <> '')
);

CREATE UNIQUE INDEX idx_dev_machine_scope_workspace
    ON dev_machine_scope_settings(workspace_id)
    WHERE team_id IS NULL AND project_id IS NULL AND issue_id IS NULL;
CREATE UNIQUE INDEX idx_dev_machine_scope_team
    ON dev_machine_scope_settings(team_id)
    WHERE team_id IS NOT NULL;
CREATE UNIQUE INDEX idx_dev_machine_scope_project
    ON dev_machine_scope_settings(project_id)
    WHERE project_id IS NOT NULL;
CREATE UNIQUE INDEX idx_dev_machine_scope_issue
    ON dev_machine_scope_settings(issue_id)
    WHERE issue_id IS NOT NULL;

ALTER TABLE dev_machines
    ADD COLUMN environment_id UUID REFERENCES dev_machine_environments(id) ON DELETE SET NULL,
    ADD COLUMN repository_affinity_id UUID REFERENCES github_repos(id) ON DELETE SET NULL,
    ADD COLUMN keep_running BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN environment_builder BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN delete_requested_at TIMESTAMPTZ;

UPDATE dev_machines machine
SET repository_affinity_id = repository.id
FROM github_repos repository
WHERE repository.workspace_id = machine.workspace_id
  AND LOWER(repository.full_name) = LOWER(machine.repo_owner || '/' || machine.repo_name);

UPDATE dev_machines
SET last_activity_at = COALESCE(last_activity_at, started_at, created_at);

WITH duplicates AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY workspace_id, LOWER(name)
        ORDER BY created_at, id
    ) AS position
    FROM dev_machines
)
UPDATE dev_machines machine
SET name = LEFT(machine.name, 246) || '-' || LEFT(machine.id::text, 8)
FROM duplicates
WHERE duplicates.id = machine.id AND duplicates.position > 1;

CREATE UNIQUE INDEX idx_dev_machines_workspace_name
    ON dev_machines(workspace_id, LOWER(name));
CREATE INDEX idx_dev_machines_idle
    ON dev_machines(workspace_id, last_activity_at)
    WHERE status = 'running' AND keep_running = FALSE;
CREATE INDEX idx_dev_machines_delete_requested
    ON dev_machines(delete_requested_at)
    WHERE delete_requested_at IS NOT NULL;

CREATE TABLE dev_machine_checkouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    github_repo_id UUID NOT NULL REFERENCES github_repos(id) ON DELETE RESTRICT,
    repository_full_name VARCHAR(512) NOT NULL,
    base_branch VARCHAR(255) NOT NULL,
    working_branch VARCHAR(255) NOT NULL,
    workspace_path TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'preparing', 'ready', 'failed')),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ,
    UNIQUE (machine_id, issue_id),
    UNIQUE (machine_id, workspace_path)
);

CREATE INDEX idx_dev_machine_checkouts_issue
    ON dev_machine_checkouts(workspace_id, issue_id, created_at DESC);

INSERT INTO dev_machine_checkouts (
    workspace_id, machine_id, issue_id, github_repo_id, repository_full_name,
    base_branch, working_branch, workspace_path, status, last_activity_at
)
SELECT machine.workspace_id, machine.id, machine.issue_id, machine.repository_affinity_id,
       machine.repo_owner || '/' || machine.repo_name, machine.base_branch, machine.working_branch,
       '/workspace', 'ready', COALESCE(machine.last_activity_at, machine.updated_at)
FROM dev_machines machine
WHERE machine.issue_id IS NOT NULL
  AND machine.repository_affinity_id IS NOT NULL
  AND machine.repo_owner <> '' AND machine.repo_name <> ''
ON CONFLICT (machine_id, issue_id) DO NOTHING;

CREATE TABLE dev_machine_terminal_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    checkout_id UUID REFERENCES dev_machine_checkouts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    runtime_session_name VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMPTZ,
    UNIQUE (machine_id, runtime_session_name)
);

CREATE INDEX idx_dev_machine_terminal_sessions_machine
    ON dev_machine_terminal_sessions(workspace_id, machine_id, created_at DESC);

ALTER TABLE dev_machine_agent_runs
    ADD COLUMN checkout_id UUID REFERENCES dev_machine_checkouts(id) ON DELETE SET NULL;

ALTER TABLE dev_machine_operations
    ADD COLUMN environment_id UUID REFERENCES dev_machine_environments(id) ON DELETE CASCADE,
    ADD COLUMN checkout_id UUID REFERENCES dev_machine_checkouts(id) ON DELETE CASCADE;

ALTER TABLE dev_machine_services
    DROP CONSTRAINT IF EXISTS dev_machine_services_service_type_check;
ALTER TABLE dev_machine_services
    ADD CONSTRAINT dev_machine_services_service_type_check CHECK (
        service_type IN ('ide', 'terminal', 'agent', 'browser', 'collector', 'egress')
    );

CREATE TRIGGER dev_machine_environments_touch BEFORE UPDATE ON dev_machine_environments
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
CREATE TRIGGER dev_machine_scope_settings_touch BEFORE UPDATE ON dev_machine_scope_settings
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
CREATE TRIGGER dev_machine_checkouts_touch BEFORE UPDATE ON dev_machine_checkouts
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
