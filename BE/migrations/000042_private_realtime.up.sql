-- Sticky mode protects queued/stale events even after a private team is deleted
-- or made public. Normal public-only workspaces retain full realtime behavior.
CREATE TABLE workspace_privacy (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE
);
INSERT INTO workspace_privacy(workspace_id) SELECT DISTINCT workspace_id FROM teams WHERE is_private;
CREATE FUNCTION kuayle_enable_private_realtime() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.is_private THEN
        INSERT INTO workspace_privacy(workspace_id) VALUES(NEW.workspace_id) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER private_realtime_mode AFTER INSERT OR UPDATE OF is_private ON teams
    FOR EACH ROW EXECUTE FUNCTION kuayle_enable_private_realtime();
