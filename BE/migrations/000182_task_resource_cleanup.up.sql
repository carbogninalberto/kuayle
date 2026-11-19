ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'cleanup_task';

ALTER TABLE dev_machine_checkouts DROP CONSTRAINT dev_machine_checkouts_status_check;
ALTER TABLE dev_machine_checkouts ADD CONSTRAINT dev_machine_checkouts_status_check
    CHECK (status IN ('queued', 'preparing', 'ready', 'failed', 'retired'));
