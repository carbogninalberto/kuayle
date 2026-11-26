CREATE TABLE conversation_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    parent_id uuid NOT NULL,
    team_id uuid REFERENCES teams(id) ON DELETE CASCADE,
    project_id uuid REFERENCES projects(id) ON DELETE SET NULL,
    issue_id uuid REFERENCES issue_records(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('once','interval','cron','condition')),
    timezone text NOT NULL DEFAULT 'UTC',
    spec jsonb NOT NULL,
    action text NOT NULL CHECK (action IN ('notify','continue')),
    instructions text NOT NULL CHECK (char_length(instructions) BETWEEN 1 AND 2000),
    state text NOT NULL DEFAULT 'scheduled' CHECK (state IN ('scheduled','completed','cancelled','expired')),
    next_run_at timestamptz NOT NULL,
    deadline timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    probe_run_id uuid,
    condition_met boolean NOT NULL DEFAULT false,
    attempts integer NOT NULL DEFAULT 0,
    fire_count integer NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    last_outcome text NOT NULL DEFAULT 'Registered',
    fired_at timestamptz,
    FOREIGN KEY (conversation_id,workspace_id) REFERENCES conversations(id,workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id,conversation_id) REFERENCES conversation_messages(id,conversation_id) ON DELETE CASCADE
);
CREATE INDEX conversation_jobs_due ON conversation_jobs(next_run_at) WHERE state='scheduled';
CREATE INDEX conversation_jobs_active_workspace ON conversation_jobs(workspace_id) WHERE state='scheduled';
CREATE INDEX conversation_jobs_active_user ON conversation_jobs(user_id) WHERE state='scheduled';
CREATE INDEX conversation_jobs_history ON conversation_jobs(conversation_id,parent_id);
-- Removing a job must also remove its queued attempt so it cannot run as an ordinary turn.
ALTER TABLE conversation_messages ADD COLUMN scheduled_probe jsonb, ADD COLUMN scheduled_job_id uuid REFERENCES conversation_jobs(id) ON DELETE CASCADE;
