-- Rollback keeps the ports and routes preview ports to the developer
-- container again. Nothing listens there for a preview that runs in its own
-- container, so such a preview must be started again after the rollback.
UPDATE dev_machine_ports SET target = 'developer', updated_at = now() WHERE target = 'preview';
ALTER TABLE dev_machine_ports DROP CONSTRAINT dev_machine_ports_target_check,
 ADD CONSTRAINT dev_machine_ports_target_check CHECK (target IN ('developer', 'docker'));
