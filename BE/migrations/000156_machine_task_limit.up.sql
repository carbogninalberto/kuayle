-- A machine can take its own agent task limit instead of the workspace
-- policy's task_machine_max_tasks. NULL means the policy applies; 1 to 10
-- matches the policy's own range. The limit only decides where later tasks are
-- placed, so it can change while the machine runs.
ALTER TABLE dev_machines ADD COLUMN max_tasks integer NULL
    CHECK (max_tasks IS NULL OR max_tasks BETWEEN 1 AND 10);
