DROP INDEX IF EXISTS idx_dev_machine_log_chunks_agent_run_cursor;
DROP INDEX IF EXISTS idx_dev_machine_events_agent_run_cursor;

ALTER TABLE dev_machine_log_chunks
    DROP CONSTRAINT IF EXISTS dev_machine_log_chunks_run_sequence_key;

ALTER TABLE dev_machine_log_chunks
    ADD CONSTRAINT dev_machine_log_chunks_machine_run_service_stream_sequence_key
    UNIQUE NULLS NOT DISTINCT (machine_id, agent_run_id, service_id, stream, sequence);
