-- Per-machine Docker daemons (opt-in per workspace) and uncapped app ports.

-- Workspace administrators opt machines into a sandboxed Docker daemon. Off
-- by default; it applies to machines created while it is on, and starting a
-- machine with a daemon is refused once it is turned off.
ALTER TABLE dev_machine_workspace_policies
 ADD COLUMN allow_docker boolean NOT NULL DEFAULT false;

-- The daemon runs as the machine's "docker" service container.
ALTER TABLE dev_machine_services DROP CONSTRAINT dev_machine_services_service_type_check,
 ADD CONSTRAINT dev_machine_services_service_type_check CHECK (
  service_type IN ('ide', 'terminal', 'agent', 'browser', 'collector', 'egress', 'docker'));

-- An app port is served by the developer container or, for ports published
-- by containers of the machine's daemon, by the docker service container.
ALTER TABLE dev_machine_ports
 ADD COLUMN target text NOT NULL DEFAULT 'developer' CHECK (target IN ('developer', 'docker'));

-- The daemon's published ports from the latest scan. listening_ports holds
-- every listening port of the machine (developer and daemon).
ALTER TABLE dev_machines
 ADD COLUMN docker_listening_ports int[] NOT NULL DEFAULT '{}';
