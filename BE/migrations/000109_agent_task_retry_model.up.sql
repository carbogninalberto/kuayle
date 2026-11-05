-- A retry receipt records the model and model server the caller asked for
-- ('' and NULL keep the task's), so replaying a request ID with another model
-- or server conflicts instead of returning the first attempt. The server is not
-- a foreign key: the receipt keeps what was asked even after the server is deleted.
ALTER TABLE agent_task_retries
    ADD COLUMN model text NOT NULL DEFAULT '' CHECK (char_length(model) <= 200),
    ADD COLUMN model_server_id uuid;
