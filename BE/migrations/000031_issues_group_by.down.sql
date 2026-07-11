ALTER TABLE user_preferences
    DROP CONSTRAINT IF EXISTS user_preferences_issues_group_by_check,
    DROP COLUMN IF EXISTS issues_group_by;
