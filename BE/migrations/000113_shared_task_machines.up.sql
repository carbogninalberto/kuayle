-- Agent tasks of one user share a running dev machine, each in its own
-- worktree, up to task_machine_max_tasks active tasks per machine (2 by
-- default, at most 4). A new task machine is created only when every suitable
-- machine is at that limit.
ALTER TABLE dev_machine_workspace_policies
  ADD COLUMN task_machine_max_tasks int NOT NULL DEFAULT 2
  CHECK (task_machine_max_tasks BETWEEN 1 AND 4);

-- Attempts of different tasks now run on one machine at the same time. The
-- user's own (non-task) agent runs keep a machine to themselves.
DROP INDEX idx_dev_machine_agent_runs_one_active;
CREATE UNIQUE INDEX idx_dev_machine_agent_runs_one_active_manual ON dev_machine_agent_runs(machine_id)
    WHERE task_id IS NULL AND status IN ('queued', 'starting', 'running', 'waiting_input');
CREATE INDEX idx_dev_machine_agent_runs_machine_active ON dev_machine_agent_runs(machine_id)
    WHERE status IN ('queued', 'starting', 'running', 'waiting_input');
