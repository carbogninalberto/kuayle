ALTER TABLE user_preferences
    DROP COLUMN IF EXISTS team_workflow_sort_overrides,
    DROP COLUMN IF EXISTS workflow_sort_order,
    DROP COLUMN IF EXISTS workflow_sort_mode;
