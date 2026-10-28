-- Restores the previous column defaults. Limits raised by the up migration
-- are kept: they cannot be told apart from limits an admin set afterwards,
-- and lowering them could leave running machines above a limit.
ALTER TABLE dev_machine_workspace_policies
    ALTER COLUMN max_concurrent_machines SET DEFAULT 5,
    ALTER COLUMN max_machines_per_user SET DEFAULT 2;
