ALTER TABLE assistant_command_runs
    ADD COLUMN timeout_seconds integer NOT NULL DEFAULT 60
    CHECK (timeout_seconds BETWEEN 1 AND 600 AND (NOT migration OR timeout_seconds = 60));
