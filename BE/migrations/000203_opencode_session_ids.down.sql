-- Rolling back forgets the session IDs the older check refuses (OpenCode's):
-- the next attempt of those tasks starts a new session with the handoff, as
-- OpenCode attempts did before. Other IDs stay.
UPDATE dev_machine_agent_runs SET native_session_id = NULL WHERE native_session_id !~ '^[A-Za-z0-9][A-Za-z0-9-]{0,127}$';
UPDATE agent_sessions SET native_session_id = NULL WHERE native_session_id !~ '^[A-Za-z0-9][A-Za-z0-9-]{0,127}$';
ALTER TABLE dev_machine_agent_runs
    DROP CONSTRAINT dev_machine_agent_runs_native_session_id_check,
    ADD CONSTRAINT dev_machine_agent_runs_native_session_id_check CHECK (native_session_id ~ '^[A-Za-z0-9][A-Za-z0-9-]{0,127}$');
ALTER TABLE agent_sessions
    DROP CONSTRAINT agent_sessions_native_session_id_check,
    ADD CONSTRAINT agent_sessions_native_session_id_check CHECK (native_session_id ~ '^[A-Za-z0-9][A-Za-z0-9-]{0,127}$');
