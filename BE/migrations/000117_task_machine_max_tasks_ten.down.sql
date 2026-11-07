-- Limits above the former maximum of 4 become 4, so the former check holds.
ALTER TABLE dev_machine_workspace_policies
  ALTER COLUMN task_machine_max_tasks SET DEFAULT 2;
ALTER TABLE dev_machine_workspace_policies
  DROP CONSTRAINT IF EXISTS dev_machine_workspace_policies_task_machine_max_tasks_check;
UPDATE dev_machine_workspace_policies SET task_machine_max_tasks=4 WHERE task_machine_max_tasks > 4;
ALTER TABLE dev_machine_workspace_policies
  ADD CONSTRAINT dev_machine_workspace_policies_task_machine_max_tasks_check
  CHECK (task_machine_max_tasks BETWEEN 1 AND 4);
