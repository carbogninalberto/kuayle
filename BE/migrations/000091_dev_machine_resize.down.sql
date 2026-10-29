-- Rollback drops pending resizes (machines keep their applied size) and the
-- recorded resource limits. Resize operations are removed because the older
-- schema has no manager that applies them; the enum value stays because
-- PostgreSQL cannot drop enum values.
DELETE FROM dev_machine_operations WHERE action = 'resize';
ALTER TABLE dev_machines
 DROP CONSTRAINT dev_machines_resource_limit_check,
 DROP COLUMN resource_limit_at,
 DROP COLUMN resource_limit_message,
 DROP COLUMN resource_limit_kind,
 DROP CONSTRAINT dev_machines_pending_resize_check,
 DROP COLUMN resize_error,
 DROP COLUMN pending_disk_gb,
 DROP COLUMN pending_memory_mb,
 DROP COLUMN pending_cpu_millis;
