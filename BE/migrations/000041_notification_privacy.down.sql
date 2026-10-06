DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
END $$;
DROP TRIGGER notification_privacy_transition ON teams;
DROP TRIGGER notification_privacy_delete ON teams;
DROP FUNCTION kuayle_notification_privacy_cleanup();
ALTER TABLE notifications DROP COLUMN source_team_ids;
