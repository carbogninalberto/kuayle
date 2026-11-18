ALTER TABLE agent_model_servers
    ADD COLUMN model_names JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(model_names) = 'object');
