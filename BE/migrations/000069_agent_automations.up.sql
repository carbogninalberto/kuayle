-- Event-driven and scheduled coding tasks. Rules match the current issue state,
-- so a missed event or crash cannot skip or duplicate work: one run per rule
-- and issue. Runs act for a recorded user under that user's current permissions.
ALTER TABLE dev_machine_workspace_policies ADD COLUMN allow_unattended_automations boolean NOT NULL DEFAULT false;

CREATE TABLE agent_automations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
 owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
 enabled boolean NOT NULL DEFAULT true,
 label_id uuid REFERENCES labels(id) ON DELETE CASCADE,
 status_id uuid REFERENCES team_statuses(id) ON DELETE CASCADE,
 instructions text NOT NULL CHECK (char_length(instructions) BETWEEN 1 AND 4000),
 unattended boolean NOT NULL DEFAULT false,
 run_hour smallint CHECK (run_hour BETWEEN 0 AND 23),
 max_active smallint NOT NULL DEFAULT 1 CHECK (max_active BETWEEN 1 AND 5),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (label_id IS NOT NULL OR status_id IS NOT NULL)
);
CREATE INDEX agent_automations_workspace ON agent_automations(workspace_id, created_at);

CREATE TABLE agent_automation_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 automation_id uuid NOT NULL REFERENCES agent_automations(id) ON DELETE CASCADE,
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 issue_id uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
 status text NOT NULL CHECK (status IN ('pending_approval','starting','started','dismissed','failed')),
 actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
 request_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 task_id uuid REFERENCES agent_tasks(id) ON DELETE SET NULL,
 error text CHECK (octet_length(error) <= 1024),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (automation_id, issue_id),
 CHECK (status <> 'starting' OR actor_id IS NOT NULL)
);
CREATE INDEX agent_automation_runs_status ON agent_automation_runs(status, updated_at);
