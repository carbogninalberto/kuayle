-- Rollback removes the task machine resources and the 2 GiB disk step (it
-- needs the previous release, which sizes task machines as "small"). A disk
-- limit rounded down by the up migration keeps its even value.
ALTER TABLE dev_machine_workspace_policies
 DROP CONSTRAINT dev_machine_workspace_policies_task_machine_disk_limit_check,
 DROP CONSTRAINT dev_machine_workspace_policies_max_disk_step_check,
 DROP CONSTRAINT dev_machine_workspace_policies_task_machine_memory_check,
 DROP COLUMN task_machine_disk_gb,
 DROP COLUMN task_machine_memory_mb,
 DROP COLUMN task_machine_cpu_millis;
