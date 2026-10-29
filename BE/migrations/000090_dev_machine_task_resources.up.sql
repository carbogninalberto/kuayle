-- Task machine resources and disk sizes in 2 GiB steps.

-- Kuayle sizes the machines it creates itself for agent tasks (delegations
-- and automations) from the workspace policy. Existing workspaces get the new
-- default of 4 vCPU and 8 GiB (the former task machine had 2 vCPU and 4 GiB)
-- and keep the former 20 GB disk.
ALTER TABLE dev_machine_workspace_policies
 ADD COLUMN task_machine_cpu_millis int NOT NULL DEFAULT 4000 CHECK (task_machine_cpu_millis > 0),
 ADD COLUMN task_machine_memory_mb int NOT NULL DEFAULT 8192 CHECK (task_machine_memory_mb > 0),
 ADD COLUMN task_machine_disk_gb int NOT NULL DEFAULT 20 CHECK (
  task_machine_disk_gb BETWEEN 20 AND 2048 AND task_machine_disk_gb % 2 = 0),
 -- At least 1 GiB per vCPU, as for any custom machine size.
 ADD CONSTRAINT dev_machine_workspace_policies_task_machine_memory_check CHECK (
  task_machine_memory_mb::bigint * 1000 >= task_machine_cpu_millis::bigint * 1024);

-- Disk sizes and the workspace disk limit are even numbers of GB. An odd limit
-- (irreversibly) becomes the even size below it, which is still at least 20.
UPDATE dev_machine_workspace_policies SET max_disk_gb = max_disk_gb - 1 WHERE max_disk_gb % 2 = 1;
ALTER TABLE dev_machine_workspace_policies
 ADD CONSTRAINT dev_machine_workspace_policies_max_disk_step_check CHECK (max_disk_gb % 2 = 0),
 ADD CONSTRAINT dev_machine_workspace_policies_task_machine_disk_limit_check CHECK (task_machine_disk_gb <= max_disk_gb);
