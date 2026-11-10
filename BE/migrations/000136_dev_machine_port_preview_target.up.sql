-- A task's preview runs in a container of its own instead of the developer
-- container, so an app port can also be served by that preview container.
-- Existing ports keep their target: a preview started before this migration
-- keeps routing to the developer container until it is started again.
ALTER TABLE dev_machine_ports DROP CONSTRAINT dev_machine_ports_target_check,
 ADD CONSTRAINT dev_machine_ports_target_check CHECK (target IN ('developer', 'docker', 'preview'));
