ALTER FUNCTION kuayle_private_edges_valid(UUID) RENAME TO kuayle_private_asset_edges_valid;
CREATE FUNCTION kuayle_private_edges_valid(wid UUID) RETURNS BOOLEAN LANGUAGE SQL VOLATILE AS $$
SELECT kuayle_private_asset_edges_valid(wid) AND NOT EXISTS(
    SELECT 1 FROM project_status_visibility v JOIN projects p ON p.id=v.project_id
    LEFT JOIN teams pt ON pt.id=p.team_id
    JOIN team_statuses s ON s.id=v.status_id JOIN teams st ON st.id=s.team_id
    WHERE (p.workspace_id=wid OR st.workspace_id=wid)
    AND (COALESCE(pt.is_private,FALSE) OR st.is_private)
    AND (p.team_id IS DISTINCT FROM s.team_id OR p.workspace_id<>st.workspace_id)
);
$$;
CREATE FUNCTION kuayle_private_project_status_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE wid UUID; pid UUID; BEGIN
    IF TG_OP='DELETE' THEN pid:=OLD.project_id; ELSE pid:=NEW.project_id; END IF;
    SELECT workspace_id INTO wid FROM projects WHERE id=pid;
    IF wid IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(wid::TEXT,61));
        IF TG_WHEN='AFTER' AND NOT kuayle_private_edges_valid(wid) THEN
            RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER private_project_status_lock BEFORE INSERT OR UPDATE OR DELETE ON project_status_visibility
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_project_status_guard();
CREATE TRIGGER private_project_status_check AFTER INSERT OR UPDATE ON project_status_visibility
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_project_status_guard();
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM workspace_privacy WHERE NOT kuayle_private_edges_valid(workspace_id)) THEN
        RAISE EXCEPTION 'Resolve private project/status references before migration';
    END IF;
END $$;
