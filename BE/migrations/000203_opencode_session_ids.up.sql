-- OpenCode's session IDs (ses_…) contain an underscore. An attempt and its
-- task's agent session may store them; an ID still starts with a letter or
-- digit, so it can never be read as a flag (agent.ValidSessionID).
ALTER TABLE dev_machine_agent_runs
    DROP CONSTRAINT dev_machine_agent_runs_native_session_id_check,
    ADD CONSTRAINT dev_machine_agent_runs_native_session_id_check CHECK (native_session_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$');
ALTER TABLE agent_sessions
    DROP CONSTRAINT agent_sessions_native_session_id_check,
    ADD CONSTRAINT agent_sessions_native_session_id_check CHECK (native_session_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$');
