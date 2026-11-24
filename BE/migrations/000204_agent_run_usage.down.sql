-- Drops the usage the runs recorded; it cannot be recovered afterwards.
ALTER TABLE dev_machine_agent_runs
    DROP COLUMN IF EXISTS cost_usd,
    DROP COLUMN IF EXISTS cache_write_tokens,
    DROP COLUMN IF EXISTS cache_read_tokens,
    DROP COLUMN IF EXISTS output_tokens,
    DROP COLUMN IF EXISTS input_tokens;
