ALTER TABLE agent_automations
    ADD COLUMN workflow_id uuid,
    ADD COLUMN pinned_version integer,
    ADD COLUMN cron text NOT NULL DEFAULT '',
    ADD COLUMN timezone text NOT NULL DEFAULT 'UTC',
    ADD COLUMN schedule_error text,
    ADD COLUMN next_run_at timestamptz,
    ADD COLUMN last_checked_at timestamptz,
    ADD FOREIGN KEY (workflow_id, workspace_id) REFERENCES workflows(id, workspace_id),
    ADD FOREIGN KEY (workflow_id, pinned_version) REFERENCES workflow_versions(workflow_id, version),
    ADD CHECK (pinned_version IS NULL OR workflow_id IS NOT NULL),
    ADD CHECK ((cron = '') = (next_run_at IS NULL)),
    ADD CHECK (cron = '' OR run_hour IS NULL);
CREATE INDEX agent_automations_due ON agent_automations(next_run_at) WHERE enabled AND cron <> '';

ALTER TABLE agent_automation_runs
    ADD COLUMN attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN replays integer NOT NULL DEFAULT 0,
    ADD COLUMN approved boolean NOT NULL DEFAULT false,
    ADD COLUMN lease_id uuid,
    ADD COLUMN available_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX agent_automation_runs_ready ON agent_automation_runs(available_at,id) WHERE status='starting';
CREATE INDEX agent_automation_runs_history ON agent_automation_runs(automation_id, created_at DESC, id DESC);
