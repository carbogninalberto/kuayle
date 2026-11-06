-- Restoring one active run per machine fails while a machine still runs
-- attempts of several tasks; let them finish (or cancel them) first.
DROP INDEX IF EXISTS idx_dev_machine_agent_runs_machine_active;
DROP INDEX IF EXISTS idx_dev_machine_agent_runs_one_active_manual;
CREATE UNIQUE INDEX idx_dev_machine_agent_runs_one_active ON dev_machine_agent_runs(machine_id)
    WHERE status IN ('queued', 'starting', 'running', 'waiting_input');

ALTER TABLE dev_machine_workspace_policies DROP COLUMN task_machine_max_tasks;
