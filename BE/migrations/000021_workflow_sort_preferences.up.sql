ALTER TABLE user_preferences
    ADD COLUMN workflow_sort_mode VARCHAR(30) NOT NULL DEFAULT 'default',
    ADD COLUMN workflow_sort_order JSONB NOT NULL DEFAULT '["backlog","unstarted","started","completed","cancelled"]'::jsonb,
    ADD COLUMN team_workflow_sort_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;
