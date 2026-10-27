-- One application preview port per machine, served by the gateway on its own
-- host and proxied to the developer container. Not a container service row.
CREATE TABLE dev_machine_previews (
 machine_id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL,
 port int NOT NULL CHECK (port BETWEEN 1024 AND 65535),
 created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY (workspace_id, machine_id) REFERENCES dev_machines(workspace_id, id) ON DELETE CASCADE
);
