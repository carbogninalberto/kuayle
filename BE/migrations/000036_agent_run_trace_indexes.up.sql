DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT c.conname
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE t.relname = 'dev_machine_log_chunks' AND c.contype = 'u'
    LOOP
        EXECUTE format('ALTER TABLE dev_machine_log_chunks DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

ALTER TABLE dev_machine_log_chunks
    ADD CONSTRAINT dev_machine_log_chunks_run_sequence_key
    UNIQUE NULLS NOT DISTINCT (machine_id, agent_run_id, service_id, stream, sequence);

CREATE INDEX IF NOT EXISTS idx_dev_machine_events_agent_run_cursor
    ON dev_machine_events(agent_run_id, id)
    WHERE agent_run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dev_machine_log_chunks_agent_run_cursor
    ON dev_machine_log_chunks(agent_run_id, id)
    WHERE agent_run_id IS NOT NULL;
