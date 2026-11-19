DELETE FROM dev_machine_operations WHERE action = 'cleanup_task';

UPDATE dev_machine_checkouts SET status = 'failed' WHERE status = 'retired';
ALTER TABLE dev_machine_checkouts DROP CONSTRAINT dev_machine_checkouts_status_check;
ALTER TABLE dev_machine_checkouts ADD CONSTRAINT dev_machine_checkouts_status_check
    CHECK (status IN ('queued', 'preparing', 'ready', 'failed'));
-- PostgreSQL does not support removing an enum value in place.
