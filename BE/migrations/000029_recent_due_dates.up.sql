ALTER TABLE user_preferences
    ADD COLUMN recent_due_dates JSONB NOT NULL DEFAULT '[]'::jsonb;
