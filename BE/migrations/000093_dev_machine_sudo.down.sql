-- Rollback forgets which machines were created with sudo. Containers already
-- created with it keep their configuration until the machines are deleted, so
-- delete those machines before rolling back.
ALTER TABLE dev_machines DROP COLUMN sudo_enabled;
ALTER TABLE dev_machine_workspace_policies DROP COLUMN allow_sudo;
