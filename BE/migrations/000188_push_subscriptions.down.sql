DROP TABLE IF EXISTS push_subscriptions;

ALTER TABLE user_preferences
    DROP COLUMN IF EXISTS push_notifications;
