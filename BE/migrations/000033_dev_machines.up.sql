CREATE TYPE dev_machine_status AS ENUM (
    'configuring', 'queued', 'spawning', 'running', 'paused', 'stopping',
    'stopped', 'tearing_down', 'destroyed', 'failed', 'expired'
);

CREATE TYPE dev_machine_operation_action AS ENUM (
    'spawn', 'start', 'stop', 'pause', 'teardown', 'reconcile',
    'run_agent', 'cancel_agent'
);

CREATE TYPE dev_machine_operation_status AS ENUM (
    'pending', 'leased', 'completed', 'failed', 'cancelled'
);

CREATE TYPE dev_machine_agent_run_status AS ENUM (
    'queued', 'starting', 'running', 'waiting_input', 'succeeded',
    'failed', 'cancelled', 'timeout'
);

CREATE TYPE dev_machine_agent_run_step_status AS ENUM (
    'queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped'
);

CREATE TYPE dev_machine_access_ticket_status AS ENUM (
    'active', 'used', 'expired', 'revoked'
);

CREATE TABLE dev_machine_workspace_policies (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    max_concurrent_machines INT NOT NULL DEFAULT 5 CHECK (max_concurrent_machines >= 0),
    max_machines_per_user INT NOT NULL DEFAULT 2 CHECK (max_machines_per_user >= 0),
    max_daily_agent_runs INT NOT NULL DEFAULT 25 CHECK (max_daily_agent_runs >= 0),
    max_runtime_minutes INT NOT NULL DEFAULT 480 CHECK (max_runtime_minutes BETWEEN 5 AND 1440),
    max_disk_gb INT NOT NULL DEFAULT 100 CHECK (max_disk_gb BETWEEN 20 AND 2048),
    allowed_providers JSONB NOT NULL DEFAULT '["claude-code","opencode","codex"]'::jsonb,
    allowed_repositories JSONB NOT NULL DEFAULT '[]'::jsonb,
    allow_custom_providers BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE dev_machines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    routing_key VARCHAR(32) NOT NULL UNIQUE CHECK (routing_key ~ '^[a-z0-9]{12,32}$'),
    name VARCHAR(255) NOT NULL,
    status dev_machine_status NOT NULL DEFAULT 'configuring',
    desired_status dev_machine_status NOT NULL DEFAULT 'queued',
    generation BIGINT NOT NULL DEFAULT 1,
    repo_url TEXT NOT NULL,
    repo_provider VARCHAR(32) NOT NULL DEFAULT 'github',
    repo_owner VARCHAR(255) NOT NULL,
    repo_name VARCHAR(255) NOT NULL,
    base_branch VARCHAR(255) NOT NULL DEFAULT 'main',
    working_branch VARCHAR(255) NOT NULL,
    machine_size VARCHAR(16) NOT NULL CHECK (machine_size IN ('small', 'medium', 'large')),
    cpu_millis INT NOT NULL CHECK (cpu_millis > 0),
    memory_mb INT NOT NULL CHECK (memory_mb > 0),
    disk_gb INT NOT NULL CHECK (disk_gb > 0),
    pids_limit INT NOT NULL DEFAULT 512 CHECK (pids_limit > 0),
    max_runtime_minutes INT NOT NULL CHECK (max_runtime_minutes BETWEEN 1 AND 1440),
    docker_network_name VARCHAR(255),
    workspace_volume_name VARCHAR(255),
    services_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_error_code VARCHAR(128),
    last_error_message TEXT,
    last_activity_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    teardown_at TIMESTAMPTZ,
    destroyed_at TIMESTAMPTZ
);

CREATE INDEX idx_dev_machines_workspace_status ON dev_machines(workspace_id, status);
CREATE INDEX idx_dev_machines_workspace_creator ON dev_machines(workspace_id, created_by_user_id);
CREATE INDEX idx_dev_machines_issue ON dev_machines(issue_id) WHERE issue_id IS NOT NULL;
CREATE INDEX idx_dev_machines_expiry ON dev_machines(expires_at)
    WHERE status NOT IN ('destroyed', 'tearing_down');

CREATE TABLE dev_machine_agent_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    provider_id VARCHAR(64) NOT NULL,
    display_name VARCHAR(128) NOT NULL,
    image_ref TEXT NOT NULL,
    supported_modes JSONB NOT NULL DEFAULT '["interactive","autonomous"]'::jsonb,
    required_secrets JSONB NOT NULL DEFAULT '[]'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_custom BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (machine_id, provider_id)
);

CREATE TABLE dev_machine_services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID,
    service_type VARCHAR(32) NOT NULL CHECK (
        service_type IN ('ide', 'agent', 'browser', 'collector', 'egress')
    ),
    service_key VARCHAR(64) NOT NULL,
    container_id VARCHAR(255),
    container_name VARCHAR(255) NOT NULL,
    image_ref TEXT NOT NULL,
    internal_host VARCHAR(255) NOT NULL,
    internal_port INT NOT NULL CHECK (internal_port BETWEEN 1 AND 65535),
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    health_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    health_message TEXT,
    started_at TIMESTAMPTZ,
    stopped_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (machine_id, service_key)
);

CREATE INDEX idx_dev_machine_services_machine ON dev_machine_services(machine_id, service_type);

CREATE TABLE dev_machine_volumes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    volume_type VARCHAR(32) NOT NULL CHECK (volume_type IN ('workspace', 'scratch', 'artifacts')),
    runtime_name VARCHAR(255) NOT NULL,
    mount_path TEXT NOT NULL,
    size_limit_bytes BIGINT NOT NULL CHECK (size_limit_bytes > 0),
    current_size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (current_size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE (machine_id, runtime_name)
);

CREATE TABLE dev_machine_env_vars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID,
    provider_id VARCHAR(64),
    target_service VARCHAR(32) NOT NULL,
    name VARCHAR(255) NOT NULL CHECK (name ~ '^[A-Za-z_][A-Za-z0-9_]*$'),
    encrypted_value TEXT NOT NULL,
    encryption_key_version INT NOT NULL DEFAULT 1,
    is_secret BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (machine_id, agent_run_id, provider_id, target_service, name)
);

CREATE TABLE dev_machine_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    scopes JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dev_machine_tokens_active ON dev_machine_tokens(token_hash, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE dev_machine_operations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    action dev_machine_operation_action NOT NULL,
    status dev_machine_operation_status NOT NULL DEFAULT 'pending',
    generation BIGINT NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    lease_owner VARCHAR(255),
    lease_expires_at TIMESTAMPTZ,
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    error_code VARCHAR(128),
    error_message TEXT,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    UNIQUE (machine_id, idempotency_key)
);

CREATE INDEX idx_dev_machine_operations_ready ON dev_machine_operations(available_at, created_at)
    WHERE status = 'pending';
CREATE INDEX idx_dev_machine_operations_lease ON dev_machine_operations(lease_expires_at)
    WHERE status = 'leased';

CREATE TABLE dev_machine_agent_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    requested_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    provider_id VARCHAR(64) NOT NULL,
    mode VARCHAR(16) NOT NULL CHECK (mode IN ('interactive', 'autonomous')),
    status dev_machine_agent_run_status NOT NULL DEFAULT 'queued',
    prompt TEXT NOT NULL,
    acceptance_criteria JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_commands JSONB NOT NULL DEFAULT '[]'::jsonb,
    forbidden_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_secrets JSONB NOT NULL DEFAULT '[]'::jsonb,
    test_command JSONB,
    command_argv JSONB NOT NULL,
    max_runtime_seconds INT NOT NULL CHECK (max_runtime_seconds BETWEEN 1 AND 86400),
    push_branch BOOLEAN NOT NULL DEFAULT TRUE,
    open_pull_request BOOLEAN NOT NULL DEFAULT FALSE,
    result JSONB,
    summary TEXT,
    changed_files JSONB NOT NULL DEFAULT '[]'::jsonb,
    commits JSONB NOT NULL DEFAULT '[]'::jsonb,
    branch VARCHAR(255),
    pull_request_url TEXT,
    tests_run JSONB NOT NULL DEFAULT '[]'::jsonb,
    test_status VARCHAR(16) NOT NULL DEFAULT 'not_run' CHECK (test_status IN ('passed', 'failed', 'not_run')),
    risk_notes JSONB NOT NULL DEFAULT '[]'::jsonb,
    exit_code INT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ
);

ALTER TABLE dev_machine_operations
    ADD CONSTRAINT fk_dev_machine_operations_agent_run
    FOREIGN KEY (agent_run_id) REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE;

ALTER TABLE dev_machine_services
    ADD CONSTRAINT fk_dev_machine_services_agent_run
    FOREIGN KEY (agent_run_id) REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE;
ALTER TABLE dev_machine_env_vars
    ADD CONSTRAINT fk_dev_machine_env_vars_agent_run
    FOREIGN KEY (agent_run_id) REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE;
ALTER TABLE dev_machine_tokens
    ADD CONSTRAINT fk_dev_machine_tokens_agent_run
    FOREIGN KEY (agent_run_id) REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE;

CREATE INDEX idx_dev_machine_agent_runs_machine ON dev_machine_agent_runs(machine_id, created_at DESC);
CREATE INDEX idx_dev_machine_agent_runs_workspace ON dev_machine_agent_runs(workspace_id, created_at DESC);
CREATE INDEX idx_dev_machine_agent_runs_active ON dev_machine_agent_runs(status, created_at)
    WHERE status IN ('queued', 'starting', 'running', 'waiting_input');
CREATE UNIQUE INDEX idx_dev_machine_agent_runs_one_active ON dev_machine_agent_runs(machine_id)
    WHERE status IN ('queued', 'starting', 'running', 'waiting_input');

CREATE TABLE dev_machine_agent_run_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_run_id UUID NOT NULL REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE,
    sequence INT NOT NULL,
    step_type VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status dev_machine_agent_run_step_status NOT NULL DEFAULT 'queued',
    command_argv JSONB,
    summary TEXT,
    exit_code INT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (agent_run_id, sequence)
);

CREATE TABLE dev_machine_events (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    source VARCHAR(32) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dev_machine_events_cursor ON dev_machine_events(machine_id, id DESC);
CREATE INDEX idx_dev_machine_events_issue_timeline ON dev_machine_events(workspace_id, occurred_at DESC);

CREATE TABLE dev_machine_log_chunks (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID REFERENCES dev_machine_agent_runs(id) ON DELETE CASCADE,
    service_id UUID REFERENCES dev_machine_services(id) ON DELETE CASCADE,
    stream VARCHAR(16) NOT NULL CHECK (stream IN ('stdout', 'stderr', 'pty', 'system')),
    sequence BIGINT NOT NULL,
    content TEXT NOT NULL,
    truncated BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (machine_id, agent_run_id, service_id, stream, sequence)
);

CREATE INDEX idx_dev_machine_log_chunks_cursor ON dev_machine_log_chunks(machine_id, id);

CREATE TABLE dev_machine_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID REFERENCES dev_machine_agent_runs(id) ON DELETE SET NULL,
    artifact_type VARCHAR(64) NOT NULL,
    name VARCHAR(512) NOT NULL,
    storage_key TEXT NOT NULL,
    content_type VARCHAR(255),
    size_bytes BIGINT CHECK (size_bytes IS NULL OR size_bytes >= 0),
    checksum_sha256 VARCHAR(64),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE dev_machine_git_refs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    agent_run_id UUID REFERENCES dev_machine_agent_runs(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    ref_type VARCHAR(16) NOT NULL CHECK (ref_type IN ('branch', 'commit', 'pull_request')),
    repository_full_name VARCHAR(512) NOT NULL,
    ref_name VARCHAR(512),
    commit_sha VARCHAR(64),
    pull_request_number INT,
    url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dev_machine_git_refs_issue ON dev_machine_git_refs(issue_id, created_at DESC);

CREATE TABLE dev_machine_access_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES dev_machine_services(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    status dev_machine_access_ticket_status NOT NULL DEFAULT 'active',
    bound_host VARCHAR(512) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_dev_machine_access_tickets_expiry ON dev_machine_access_tickets(expires_at)
    WHERE status = 'active';

CREATE TABLE dev_machine_access_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES dev_machine_services(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    bound_host VARCHAR(512) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_dev_machine_access_sessions_active ON dev_machine_access_sessions(token_hash, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE dev_machine_access_logs (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    machine_id UUID REFERENCES dev_machines(id) ON DELETE SET NULL,
    service_id UUID REFERENCES dev_machine_services(id) ON DELETE SET NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    decision VARCHAR(16) NOT NULL CHECK (decision IN ('allowed', 'denied')),
    reason VARCHAR(128),
    method VARCHAR(16) NOT NULL,
    path TEXT NOT NULL,
    response_status INT,
    remote_ip INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dev_machine_access_logs_machine ON dev_machine_access_logs(machine_id, created_at DESC);

CREATE TABLE dev_machine_resource_samples (
    id BIGSERIAL PRIMARY KEY,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    cpu_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
    memory_bytes BIGINT NOT NULL DEFAULT 0,
    disk_bytes BIGINT NOT NULL DEFAULT 0,
    pids INT NOT NULL DEFAULT 0,
    network_rx_bytes BIGINT NOT NULL DEFAULT 0,
    network_tx_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dev_machine_resource_samples_machine ON dev_machine_resource_samples(machine_id, created_at DESC);

CREATE OR REPLACE FUNCTION touch_dev_machine_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER dev_machines_touch BEFORE UPDATE ON dev_machines
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
CREATE TRIGGER dev_machine_operations_touch BEFORE UPDATE ON dev_machine_operations
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
CREATE TRIGGER dev_machine_agent_providers_touch BEFORE UPDATE ON dev_machine_agent_providers
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
CREATE TRIGGER dev_machine_services_touch BEFORE UPDATE ON dev_machine_services
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
CREATE TRIGGER dev_machine_workspace_policies_touch BEFORE UPDATE ON dev_machine_workspace_policies
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
