-- Live machine resize and resource limit visibility.

-- A resize is queued as an operation; the manager applies it to the
-- machine's containers and then moves the pending size into cpu_millis,
-- memory_mb and disk_gb. resize_error explains the last rejected resize.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'resize';
ALTER TABLE dev_machines
 ADD COLUMN pending_cpu_millis integer,
 ADD COLUMN pending_memory_mb integer,
 ADD COLUMN pending_disk_gb integer,
 ADD COLUMN resize_error text,
 ADD CONSTRAINT dev_machines_pending_resize_check CHECK (
  num_nulls(pending_cpu_millis, pending_memory_mb, pending_disk_gb) IN (0, 3) AND
  COALESCE(pending_cpu_millis, 1) > 0 AND COALESCE(pending_memory_mb, 1) > 0 AND COALESCE(pending_disk_gb, 1) > 0);

-- The latest limit a machine container hit (an out-of-memory kill or the
-- workspace disk limit), shown on the machine until a resize clears it.
ALTER TABLE dev_machines
 ADD COLUMN resource_limit_kind text CHECK (resource_limit_kind IN ('memory', 'disk')),
 ADD COLUMN resource_limit_message text,
 ADD COLUMN resource_limit_at timestamptz,
 ADD CONSTRAINT dev_machines_resource_limit_check CHECK (
  (resource_limit_kind IS NULL) = (resource_limit_message IS NULL) AND
  (resource_limit_kind IS NULL) = (resource_limit_at IS NULL));
