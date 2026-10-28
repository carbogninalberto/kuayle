-- Rollback removes the docker service records (their containers and volumes
-- must be removed by tearing the machines down first) and the port targets:
-- ports published by a daemon fall back to the developer container. The
-- number of app ports was never limited in the schema, so it stays unlimited.
ALTER TABLE dev_machines DROP COLUMN docker_listening_ports;
ALTER TABLE dev_machine_ports DROP COLUMN target;
DELETE FROM dev_machine_services WHERE service_type = 'docker';
ALTER TABLE dev_machine_services DROP CONSTRAINT dev_machine_services_service_type_check,
 ADD CONSTRAINT dev_machine_services_service_type_check CHECK (
  service_type IN ('ide', 'terminal', 'agent', 'browser', 'collector', 'egress'));
ALTER TABLE dev_machine_workspace_policies DROP COLUMN allow_docker;
