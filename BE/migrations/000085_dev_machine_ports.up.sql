-- App ports: up to ten TCP ports of a machine's developer container, each on
-- its own authenticated gateway host. A port outlives the task that exposed
-- it; it is the reviewer's entrypoint for manual QA. Not a container service.
CREATE TABLE dev_machine_ports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL,
 machine_id uuid NOT NULL,
 port int NOT NULL CHECK (port BETWEEN 1024 AND 65535),
 label text CHECK (label IS NULL OR (char_length(label) BETWEEN 1 AND 40 AND label ~ '^[A-Za-z0-9 ._-]+$')),
 source text NOT NULL CHECK (source IN ('user','agent','task')),
 agent_task_id uuid REFERENCES agent_tasks(id) ON DELETE SET NULL,
 created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (machine_id, port),
 UNIQUE (id, port),
 FOREIGN KEY (workspace_id, machine_id) REFERENCES dev_machines(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX dev_machine_ports_task ON dev_machine_ports(agent_task_id) WHERE agent_task_id IS NOT NULL;
INSERT INTO dev_machine_ports(workspace_id,machine_id,port,source,created_by_user_id,created_at,updated_at)
SELECT workspace_id,machine_id,port,'user',created_by_user_id,created_at,updated_at FROM dev_machine_previews;
DROP TABLE dev_machine_previews;

-- The developer container's listening TCP ports, from the manager's scan,
-- without the machine's own service ports.
ALTER TABLE dev_machines
 ADD COLUMN listening_ports int[] NOT NULL DEFAULT '{}',
 ADD COLUMN listening_ports_at timestamptz;

-- A task's preview: its own process and app port. The configured port is now
-- only the preferred one, so a command may be set without it.
ALTER TABLE dev_machine_scope_settings DROP CONSTRAINT preview_complete,
 ADD CONSTRAINT preview_port_needs_command CHECK (preview_port IS NULL OR preview_command IS NOT NULL);
ALTER TABLE agent_tasks
 ADD COLUMN preview_state text CHECK (preview_state IN ('starting','running','stopped','failed')),
 ADD COLUMN preview_error text CHECK (char_length(preview_error) <= 500),
 ADD COLUMN preview_port_id uuid,
 ADD COLUMN preview_app_port int,
 ADD CONSTRAINT agent_tasks_preview_app_port CHECK ((preview_port_id IS NULL) = (preview_app_port IS NULL)),
 -- Removing the app port clears both columns.
 ADD CONSTRAINT agent_tasks_preview_port_fk FOREIGN KEY (preview_port_id, preview_app_port)
  REFERENCES dev_machine_ports(id, port) ON DELETE SET NULL;

-- Preview operations name their task; they have no agent run of their own.
ALTER TABLE dev_machine_operations ADD COLUMN agent_task_id uuid REFERENCES agent_tasks(id) ON DELETE CASCADE;
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'stop_preview';
