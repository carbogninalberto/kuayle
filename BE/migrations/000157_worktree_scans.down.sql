-- PostgreSQL cannot remove an enum value in place; the inert 'scan_worktree'
-- value stays. Queued scans cannot run on the older manager, so their
-- operations are dropped with the table.
DELETE FROM dev_machine_operations WHERE action = 'scan_worktree';
DROP TABLE IF EXISTS dev_machine_worktree_scans;
