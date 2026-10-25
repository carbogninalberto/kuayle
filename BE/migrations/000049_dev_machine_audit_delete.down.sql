-- Detachment caused by already-completed parent deletions cannot be reversed.
-- Audit rows and their event metadata are preserved in either migration direction.
DROP TRIGGER detach_workspace_machine_access_logs_before_delete ON workspaces;
DROP FUNCTION detach_workspace_machine_access_logs_before_delete();
DROP TRIGGER detach_machine_access_logs_before_delete ON dev_machines;
DROP FUNCTION detach_machine_access_logs_before_delete();
