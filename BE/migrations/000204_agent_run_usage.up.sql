-- What each agent run's harness reported it spent (KUA-255): tokens, and the
-- cost the harness estimated in US dollars. NULL means the harness did not
-- report it, which is also true of every run recorded before this migration.
-- input_tokens excludes cache reads and writes; output_tokens includes
-- reasoning. A run's values include its closing continuation.
ALTER TABLE dev_machine_agent_runs
    ADD COLUMN input_tokens BIGINT CHECK (input_tokens >= 0),
    ADD COLUMN output_tokens BIGINT CHECK (output_tokens >= 0),
    ADD COLUMN cache_read_tokens BIGINT CHECK (cache_read_tokens >= 0),
    ADD COLUMN cache_write_tokens BIGINT CHECK (cache_write_tokens >= 0),
    ADD COLUMN cost_usd NUMERIC CHECK (cost_usd >= 0);
