DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
END $$;
DROP TRIGGER private_realtime_mode ON teams;
DROP FUNCTION kuayle_enable_private_realtime();
DROP TABLE workspace_privacy;
