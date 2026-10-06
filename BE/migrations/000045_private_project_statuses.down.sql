DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
END $$;
DROP TRIGGER private_project_status_check ON project_status_visibility;
DROP TRIGGER private_project_status_lock ON project_status_visibility;
DROP FUNCTION kuayle_private_project_status_guard();
DROP FUNCTION kuayle_private_edges_valid(UUID);
ALTER FUNCTION kuayle_private_asset_edges_valid(UUID) RENAME TO kuayle_private_edges_valid;
