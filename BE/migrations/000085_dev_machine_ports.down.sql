-- Rollback keeps one preview per machine (its lowest app port). Other app
-- ports, task preview state, and preview commands configured without a port
-- are discarded. The stop_preview enum value cannot be removed; it is unused.
CREATE TABLE dev_machine_previews (
 machine_id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL,
 port int NOT NULL CHECK (port BETWEEN 1024 AND 65535),
 created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY (workspace_id, machine_id) REFERENCES dev_machines(workspace_id, id) ON DELETE CASCADE
);
INSERT INTO dev_machine_previews(machine_id,workspace_id,port,created_by_user_id,created_at,updated_at)
SELECT DISTINCT ON (machine_id) machine_id,workspace_id,port,created_by_user_id,created_at,updated_at
FROM dev_machine_ports ORDER BY machine_id,port;
DELETE FROM dev_machine_operations WHERE action::text='stop_preview';
ALTER TABLE dev_machine_operations DROP COLUMN agent_task_id;
ALTER TABLE agent_tasks DROP COLUMN preview_app_port, DROP COLUMN preview_port_id, DROP COLUMN preview_error, DROP COLUMN preview_state;
UPDATE dev_machine_scope_settings SET preview_command=NULL WHERE preview_command IS NOT NULL AND preview_port IS NULL;
ALTER TABLE dev_machine_scope_settings DROP CONSTRAINT preview_port_needs_command,
 ADD CONSTRAINT preview_complete CHECK ((preview_command IS NULL) = (preview_port IS NULL));
ALTER TABLE dev_machines DROP COLUMN listening_ports_at, DROP COLUMN listening_ports;
DROP TABLE dev_machine_ports;
