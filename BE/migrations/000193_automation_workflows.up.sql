ALTER TABLE agent_automations
    ADD COLUMN workflow_id uuid,
    ADD COLUMN pinned_version integer,
    ADD COLUMN cron text NOT NULL DEFAULT '',
    ADD COLUMN timezone text NOT NULL DEFAULT 'UTC',
    ADD COLUMN next_run_at timestamptz,
    ADD COLUMN last_checked_at timestamptz,
    ADD FOREIGN KEY (workflow_id, workspace_id) REFERENCES workflows(id, workspace_id) ON DELETE CASCADE,
    ADD FOREIGN KEY (workflow_id, pinned_version) REFERENCES workflow_versions(workflow_id, version) ON DELETE CASCADE,
    ADD CHECK (pinned_version IS NULL OR workflow_id IS NOT NULL),
    ADD CHECK ((cron = '') = (next_run_at IS NULL)),
    ADD CHECK (cron = '' OR run_hour IS NULL);
CREATE INDEX agent_automations_due ON agent_automations(next_run_at) WHERE enabled AND cron <> '';
