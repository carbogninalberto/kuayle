ALTER TABLE assets ADD COLUMN team_id UUID REFERENCES teams(id) ON DELETE CASCADE;
CREATE INDEX idx_assets_team ON assets(team_id) WHERE team_id IS NOT NULL;

-- Extract protected attachment references from every team-owned rich text field.
-- Do not trust filenames, uploader identity, or a signed token as ownership.
CREATE VIEW private_asset_references AS
WITH content AS (
    SELECT workspace_id, team_id, description AS body FROM issues
    UNION ALL SELECT i.workspace_id, i.team_id, c.body FROM comments c JOIN issues i ON i.id=c.issue_id
    UNION ALL SELECT i.workspace_id, i.team_id, CONCAT(h.old_value, ' ', h.new_value) FROM issue_history h JOIN issues i ON i.id=h.issue_id
    UNION ALL SELECT workspace_id, team_id, description FROM projects
    UNION ALL SELECT workspace_id, team_id, description FROM issue_templates
    UNION ALL SELECT t.workspace_id, c.team_id, c.description FROM cycles c JOIN teams t ON t.id=c.team_id
    UNION ALL SELECT workspace_id, id, description FROM teams
)
SELECT DISTINCT content.workspace_id, content.team_id, (match[1])::UUID AS asset_id
FROM content CROSS JOIN LATERAL regexp_matches(COALESCE(body,''),
    '/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})', 'g') AS match;

-- Only unambiguous existing references can be assigned automatically. Unowned
-- or shared legacy uploads retain NULL and become unavailable in privacy mode.
UPDATE assets a SET team_id=refs.team_id FROM (
    SELECT asset_id, MIN(team_id::TEXT)::UUID AS team_id
    FROM private_asset_references
    GROUP BY asset_id HAVING COUNT(DISTINCT team_id)=1 AND COUNT(*)=COUNT(team_id)
) refs JOIN teams t ON t.id=refs.team_id
WHERE a.id=refs.asset_id AND a.workspace_id=t.workspace_id;

ALTER FUNCTION kuayle_private_edges_valid(UUID) RENAME TO kuayle_private_resource_edges_valid;
CREATE FUNCTION kuayle_private_edges_valid(wid UUID) RETURNS BOOLEAN
LANGUAGE SQL VOLATILE AS $$
SELECT kuayle_private_resource_edges_valid(wid) AND NOT EXISTS (
    SELECT 1 FROM private_asset_references ref
    JOIN assets a ON a.id=ref.asset_id
    LEFT JOIN teams source ON source.id=ref.team_id
    LEFT JOIN teams owner ON owner.id=a.team_id
    WHERE (ref.workspace_id=wid OR a.workspace_id=wid)
    AND (COALESCE(source.is_private,FALSE) OR COALESCE(owner.is_private,FALSE))
    AND (ref.team_id IS DISTINCT FROM a.team_id OR ref.workspace_id<>a.workspace_id)
);
$$;

CREATE TRIGGER private_boundary_lock BEFORE INSERT OR UPDATE OR DELETE ON comments
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_boundary_guard();
CREATE TRIGGER private_boundary_check AFTER INSERT OR UPDATE ON comments
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_boundary_guard();

CREATE TRIGGER private_boundary_lock BEFORE INSERT OR UPDATE OR DELETE ON issue_history
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_boundary_guard();
CREATE TRIGGER private_boundary_check AFTER INSERT OR UPDATE ON issue_history
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_boundary_guard();

CREATE FUNCTION kuayle_asset_scope_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_WHEN='AFTER' THEN
        IF NOT kuayle_private_edges_valid(NEW.workspace_id) THEN
            RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.workspace_id::TEXT,61));
    IF TG_OP='UPDATE' AND (OLD.team_id IS DISTINCT FROM NEW.team_id OR OLD.workspace_id<>NEW.workspace_id) THEN
        RAISE EXCEPTION 'Attachment ownership cannot be changed' USING ERRCODE='23514';
    END IF;
    IF NEW.team_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM teams WHERE id=NEW.team_id AND workspace_id=NEW.workspace_id) THEN
        RAISE EXCEPTION 'Attachment team must belong to workspace' USING ERRCODE='23514';
    END IF;
    IF NEW.team_id IS NULL AND EXISTS(SELECT 1 FROM workspace_privacy WHERE workspace_id=NEW.workspace_id) THEN
        RAISE EXCEPTION 'Attachments require team ownership in private-team workspaces' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER asset_scope_guard BEFORE INSERT OR UPDATE ON assets
    FOR EACH ROW EXECUTE FUNCTION kuayle_asset_scope_guard();
CREATE TRIGGER asset_scope_check AFTER INSERT OR UPDATE ON assets
    FOR EACH ROW EXECUTE FUNCTION kuayle_asset_scope_guard();
