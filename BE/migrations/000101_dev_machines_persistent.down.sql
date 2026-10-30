-- Rollback restores the machine lifetime columns. Machines get the previous
-- default runtime of 480 minutes and a lifetime of one day counted from the
-- rollback, after which the previous release tears them down; idle machines
-- are paused again after the default 240 minutes. Policies return to those
-- defaults, and machine credentials expire with the restored lifetime.
-- Machines rescued from a pending expiry teardown stay stopped.
ALTER TABLE dev_machine_workspace_policies
    ADD COLUMN max_runtime_minutes INT NOT NULL DEFAULT 480 CHECK (max_runtime_minutes BETWEEN 5 AND 1440),
    ADD COLUMN idle_pause_minutes INT NOT NULL DEFAULT 240 CHECK (idle_pause_minutes BETWEEN 5 AND 10080);
ALTER TABLE dev_machines
    ADD COLUMN max_runtime_minutes INT NOT NULL DEFAULT 480 CHECK (max_runtime_minutes BETWEEN 1 AND 1440),
    ADD COLUMN keep_running BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '1 day';
ALTER TABLE dev_machines
    ALTER COLUMN max_runtime_minutes DROP DEFAULT,
    ALTER COLUMN expires_at DROP DEFAULT;
CREATE INDEX idx_dev_machines_expiry ON dev_machines(expires_at)
    WHERE status NOT IN ('destroyed', 'tearing_down');
CREATE INDEX idx_dev_machines_idle
    ON dev_machines(workspace_id, last_activity_at)
    WHERE status = 'running' AND keep_running = FALSE;

UPDATE dev_machine_env_vars e SET expires_at = m.expires_at
FROM dev_machines m
WHERE e.machine_id = m.id AND e.expires_at IS NULL AND e.agent_run_id IS NULL AND e.name IN (
    'KUAYLE_MACHINE_TOKEN', 'KUAYLE_BROWSER_CDP_TOKEN',
    'KUAYLE_DOCKER_TLS_CA', 'KUAYLE_DOCKER_TLS_CERT', 'KUAYLE_DOCKER_TLS_KEY',
    'KUAYLE_DOCKER_CA', 'KUAYLE_DOCKER_CERT', 'KUAYLE_DOCKER_KEY');
UPDATE dev_machine_tokens t SET expires_at = m.expires_at
FROM dev_machines m WHERE t.machine_id = m.id AND t.expires_at IS NULL;
ALTER TABLE dev_machine_tokens ALTER COLUMN expires_at SET NOT NULL;
