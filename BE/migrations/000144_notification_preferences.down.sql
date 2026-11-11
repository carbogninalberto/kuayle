DROP INDEX IF EXISTS agent_task_transitions_notification_pending;
ALTER TABLE agent_task_transitions
    DROP COLUMN IF EXISTS notification_pending;

ALTER TABLE user_preferences
    DROP CONSTRAINT IF EXISTS user_preferences_notification_events_check,
    DROP CONSTRAINT IF EXISTS user_preferences_notification_sound_check,
    DROP COLUMN IF EXISTS notification_events,
    DROP COLUMN IF EXISTS notification_sound,
    DROP COLUMN IF EXISTS browser_notifications;
