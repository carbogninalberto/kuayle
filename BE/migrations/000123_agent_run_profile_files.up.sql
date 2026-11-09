-- Files Kuayle writes into an agent run's home before the harness starts: the
-- instructions and skills of the agent profile the task's workflow step runs
-- (see domain.ProfileFiles). Recorded with the run so the manager can write
-- them when it starts the container, and so a retry writes the same files.
ALTER TABLE dev_machine_agent_runs ADD COLUMN profile_files JSONB NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(profile_files) = 'array' AND octet_length(profile_files::text) <= 2097152);
