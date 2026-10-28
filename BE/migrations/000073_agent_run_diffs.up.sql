-- The attempt's diff against its base branch, captured by the manager when the
-- native run ends, so reviewers can read it before anything is published.
ALTER TABLE dev_machine_agent_runs
 ADD COLUMN diff text NOT NULL DEFAULT '' CHECK (octet_length(diff) <= 1048576),
 ADD COLUMN diff_truncated boolean NOT NULL DEFAULT false;
