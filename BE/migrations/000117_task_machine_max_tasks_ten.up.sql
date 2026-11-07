-- Up to ten agent tasks may share one task machine (a large machine runs ten
-- tasks in parallel), and new workspaces let ten share it by default. Saved
-- limits are kept.
ALTER TABLE dev_machine_workspace_policies
  DROP CONSTRAINT IF EXISTS dev_machine_workspace_policies_task_machine_max_tasks_check;
ALTER TABLE dev_machine_workspace_policies
  ADD CONSTRAINT dev_machine_workspace_policies_task_machine_max_tasks_check
  CHECK (task_machine_max_tasks BETWEEN 1 AND 10);
ALTER TABLE dev_machine_workspace_policies
  ALTER COLUMN task_machine_max_tasks SET DEFAULT 10;
