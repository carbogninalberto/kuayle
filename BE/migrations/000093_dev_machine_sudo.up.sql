-- Opt-in sudo in Dev Machines.

-- Workspace administrators allow passwordless sudo in the developer (IDE and
-- terminal) containers of new machines. Off by default.
ALTER TABLE dev_machine_workspace_policies
 ADD COLUMN allow_sudo boolean NOT NULL DEFAULT false;

-- Whether a machine was created while sudo was allowed. Its developer
-- containers are created from this record and keep it for their lifetime;
-- starting such a machine is refused once the workspace turns sudo off.
ALTER TABLE dev_machines
 ADD COLUMN sudo_enabled boolean NOT NULL DEFAULT false;
