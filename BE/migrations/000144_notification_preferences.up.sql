-- Browser notification preferences (KUA-122). notification_events maps an
-- event key to whether it notifies; a missing key reads as enabled.
ALTER TABLE user_preferences
    ADD COLUMN browser_notifications BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN notification_sound VARCHAR(20) NOT NULL DEFAULT 'chime',
    ADD COLUMN notification_events JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT user_preferences_notification_sound_check CHECK (notification_sound IN ('off', 'chime', 'bell', 'pop', 'drop', 'pulse')),
    ADD CONSTRAINT user_preferences_notification_events_check CHECK (jsonb_typeof(notification_events) = 'object');

-- A transition into a finished or review state notifies the task's creator
-- once: the transition is recorded as pending and a worker claims it after
-- the transaction commits. Existing transitions are not announced.
ALTER TABLE agent_task_transitions
    ADD COLUMN notification_pending BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX agent_task_transitions_notification_pending ON agent_task_transitions(sequence) WHERE notification_pending;
