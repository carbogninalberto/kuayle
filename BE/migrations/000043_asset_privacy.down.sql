DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
    IF EXISTS(SELECT 1 FROM assets a JOIN workspace_privacy p ON p.workspace_id=a.workspace_id WHERE a.team_id IS NULL) THEN
        RAISE EXCEPTION 'Cannot expose quarantined legacy attachments; remove them before rollback';
    END IF;
END $$;
DROP TRIGGER asset_scope_check ON assets;
DROP TRIGGER asset_scope_guard ON assets;
DROP FUNCTION kuayle_asset_scope_guard();
DROP TRIGGER private_boundary_lock ON comments;
DROP TRIGGER private_boundary_check ON comments;
DROP TRIGGER private_boundary_lock ON issue_history;
DROP TRIGGER private_boundary_check ON issue_history;
DROP FUNCTION kuayle_private_edges_valid(UUID);
ALTER FUNCTION kuayle_private_resource_edges_valid(UUID) RENAME TO kuayle_private_edges_valid;
DROP VIEW private_asset_references;
DROP INDEX idx_assets_team;
ALTER TABLE assets DROP COLUMN team_id;
