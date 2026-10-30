-- Dev machines no longer expire, pause when idle or stop after a maximum
-- runtime: a booted machine stays up until a user pauses, stops, tears down or
-- deletes it. The lifetime columns and the idle preference go away.

-- A machine the old expiry reaper marked expired but whose teardown has not
-- started yet (the operation is still pending, never leased) is rescued: the
-- teardown is cancelled and the manager stops the machine instead, keeping
-- its workspace volume. Teardowns already running are left to finish.
UPDATE dev_machine_operations
SET status = 'cancelled', completed_at = NOW(), updated_at = NOW(),
    error_code = 'machine_lifetime_removed',
    error_message = 'Dev machines no longer expire; the machine was stopped instead of torn down'
WHERE action = 'teardown' AND status = 'pending' AND idempotency_key LIKE 'ttl-teardown:%';

UPDATE dev_machines m
SET desired_status = 'stopped', updated_at = NOW()
WHERE m.status = 'expired' AND m.desired_status = 'destroyed' AND m.delete_requested_at IS NULL
  AND EXISTS (
      SELECT 1 FROM dev_machine_operations o
      WHERE o.machine_id = m.id AND o.action = 'teardown' AND o.error_code = 'machine_lifetime_removed')
  AND NOT EXISTS (
      SELECT 1 FROM dev_machine_operations o
      WHERE o.machine_id = m.id AND o.action = 'teardown' AND o.status IN ('pending', 'leased'));

-- Machine credentials used to expire with the machine. They now last until
-- the machine is torn down (which removes its containers and access).
ALTER TABLE dev_machine_tokens ALTER COLUMN expires_at DROP NOT NULL;
UPDATE dev_machine_tokens SET expires_at = NULL WHERE revoked_at IS NULL AND agent_run_id IS NULL;
UPDATE dev_machine_env_vars SET expires_at = NULL
WHERE revoked_at IS NULL AND agent_run_id IS NULL AND name IN (
    'KUAYLE_MACHINE_TOKEN', 'KUAYLE_BROWSER_CDP_TOKEN',
    'KUAYLE_DOCKER_TLS_CA', 'KUAYLE_DOCKER_TLS_CERT', 'KUAYLE_DOCKER_TLS_KEY',
    'KUAYLE_DOCKER_CA', 'KUAYLE_DOCKER_CERT', 'KUAYLE_DOCKER_KEY');

DROP INDEX IF EXISTS idx_dev_machines_expiry;
DROP INDEX IF EXISTS idx_dev_machines_idle;
ALTER TABLE dev_machines
    DROP COLUMN expires_at,
    DROP COLUMN max_runtime_minutes,
    DROP COLUMN keep_running;
ALTER TABLE dev_machine_workspace_policies
    DROP COLUMN max_runtime_minutes,
    DROP COLUMN idle_pause_minutes;
