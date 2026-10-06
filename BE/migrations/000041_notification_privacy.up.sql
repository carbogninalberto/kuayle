-- NULL means historical/unknown provenance, not workspace-public permission.
ALTER TABLE notifications ADD COLUMN source_team_ids UUID[];
UPDATE notifications n SET source_team_ids=ARRAY[i.team_id] FROM issues i WHERE i.id=n.issue_id;
ALTER TABLE notifications ADD CONSTRAINT notification_source_teams_no_nulls CHECK (source_team_ids IS NULL OR (cardinality(source_team_ids)>0 AND array_position(source_team_ids,NULL) IS NULL));

-- Legacy detached notices cannot be attributed to a team. Remove ambiguous
-- history on privatization rather than let it reappear after team deletion.
DELETE FROM notifications n WHERE source_team_ids IS NULL AND EXISTS
    (SELECT 1 FROM teams t WHERE t.workspace_id=n.workspace_id AND t.is_private);
CREATE FUNCTION kuayle_notification_privacy_cleanup() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        DELETE FROM notifications WHERE OLD.id=ANY(source_team_ids);
        RETURN OLD;
    END IF;
    IF NEW.is_private AND NOT OLD.is_private THEN
        DELETE FROM notifications WHERE workspace_id=NEW.workspace_id AND source_team_ids IS NULL;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER notification_privacy_transition AFTER UPDATE OF is_private ON teams
    FOR EACH ROW EXECUTE FUNCTION kuayle_notification_privacy_cleanup();
CREATE TRIGGER notification_privacy_delete BEFORE DELETE ON teams
    FOR EACH ROW EXECUTE FUNCTION kuayle_notification_privacy_cleanup();
