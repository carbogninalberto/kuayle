ALTER TABLE user_preferences
    ADD COLUMN issues_group_by VARCHAR(20) NOT NULL DEFAULT 'status',
    ADD CONSTRAINT user_preferences_issues_group_by_check CHECK (issues_group_by IN ('status', 'priority', 'assignee', 'project', 'none'));
