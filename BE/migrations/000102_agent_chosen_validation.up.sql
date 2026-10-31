-- An implement attempt may choose its own tests in .kuayle/validation.json.
-- The manager records the parsed file on the attempt (bounded commands and
-- summary, or the reason it was rejected), and the independent check records
-- where its commands came from, every command it ran and their exit codes,
-- and the agent's untrusted summary.
ALTER TABLE dev_machine_agent_runs
    ADD COLUMN declared_validation jsonb CHECK (declared_validation IS NULL OR (jsonb_typeof(declared_validation) = 'object' AND octet_length(declared_validation::text) <= 131072));

ALTER TABLE dev_machine_agent_run_steps
    ADD COLUMN command_source text CHECK (command_source IN ('configured', 'agent')),
    ADD COLUMN commands jsonb CHECK (commands IS NULL OR (jsonb_typeof(commands) = 'array' AND jsonb_array_length(commands) BETWEEN 1 AND 8)),
    ADD COLUMN command_results jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(command_results) = 'array' AND jsonb_array_length(command_results) <= 8),
    ADD COLUMN agent_summary text CHECK (char_length(agent_summary) <= 2000);
