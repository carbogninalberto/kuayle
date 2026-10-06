DO $$ DECLARE tab TEXT; BEGIN
 IF EXISTS(SELECT 1 FROM teams WHERE is_private) THEN
  RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
 END IF;
 FOR tab IN SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename<>'schema_migrations' LOOP
  EXECUTE format('DROP TRIGGER IF EXISTS privacy_statement_shared ON %I',tab);
 END LOOP;
END $$;
DROP TRIGGER privacy_statement_exclusive ON teams;
DROP TRIGGER privacy_statement_classification ON teams;
DROP TRIGGER privacy_statement_delete ON workspaces;
DROP TRIGGER privacy_statement_delete ON users;
DROP TRIGGER IF EXISTS privacy_statement_exclusive ON assets;
DROP FUNCTION kuayle_privacy_statement_guard();
CREATE OR REPLACE FUNCTION kuayle_private_boundary_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    row_data JSONB;
    wid UUID;
BEGIN
    IF TG_OP='DELETE' THEN row_data:=to_jsonb(OLD); ELSE row_data:=to_jsonb(NEW); END IF;
    IF TG_TABLE_NAME IN ('teams','issues','projects','issue_templates') THEN
        wid:=(row_data->>'workspace_id')::UUID;
    ELSIF TG_TABLE_NAME IN ('cycles','team_statuses') THEN
        SELECT workspace_id INTO wid FROM teams WHERE id=(row_data->>'team_id')::UUID;
    ELSE
        SELECT workspace_id INTO wid FROM issues WHERE id=(row_data->>'issue_id')::UUID;
    END IF;
    IF wid IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(wid::TEXT,61));
    END IF;
    IF TG_WHEN='BEFORE' THEN
        -- ON DELETE SET NULL must not turn private project/template content into
        -- workspace-public content. Whole-workspace deletion remains possible.
        IF TG_TABLE_NAME='teams' AND TG_OP='DELETE' AND (row_data->>'is_private')::BOOLEAN
           AND EXISTS(SELECT 1 FROM workspaces WHERE id=wid)
           AND (EXISTS(SELECT 1 FROM projects WHERE team_id=OLD.id) OR EXISTS(SELECT 1 FROM issue_templates WHERE team_id=OLD.id)) THEN
            RAISE EXCEPTION 'Delete private projects and templates before deleting their team' USING ERRCODE='23514';
        END IF;
        IF TG_OP='UPDATE' AND TG_TABLE_NAME IN ('issues','projects','issue_templates')
           AND (to_jsonb(OLD)->>'team_id') IS DISTINCT FROM (row_data->>'team_id')
           AND EXISTS(SELECT 1 FROM teams WHERE is_private AND id IN ((to_jsonb(OLD)->>'team_id')::UUID,(row_data->>'team_id')::UUID)) THEN
            RAISE EXCEPTION 'Moving resources across a private-team boundary is not supported' USING ERRCODE='23514';
        END IF;
    ELSIF wid IS NOT NULL AND NOT kuayle_private_edges_valid(wid) THEN
        RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514', HINT='Remove cross-team relationships before making the team private.';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION kuayle_asset_scope_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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

CREATE OR REPLACE FUNCTION kuayle_private_external_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE wid UUID; BEGIN
    wid:=NEW.workspace_id;
    PERFORM pg_advisory_xact_lock(hashtextextended(wid::TEXT,61));
    IF TG_TABLE_NAME='teams' THEN
        IF NEW.is_private AND (TG_OP='INSERT' OR NOT OLD.is_private) AND (
            EXISTS(SELECT 1 FROM dev_machines WHERE workspace_id=wid)
            OR EXISTS(SELECT 1 FROM dev_machine_environments WHERE workspace_id=wid)
            OR EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE workspace_id=wid)
        ) THEN
            RAISE EXCEPTION 'Remove development machines, environments and scope settings before enabling private teams' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME IN ('dev_machines','dev_machine_environments','dev_machine_scope_settings','dev_machine_workspace_policies') THEN
        IF EXISTS(SELECT 1 FROM workspace_privacy WHERE workspace_id=wid)
           AND (TG_TABLE_NAME<>'dev_machine_workspace_policies' OR (to_jsonb(NEW)->>'enabled')::BOOLEAN) THEN
            RAISE EXCEPTION 'Development machines are unavailable after private teams are enabled' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME='github_auto_transitions' THEN
        IF NEW.target_status_id IS NOT NULL AND NOT EXISTS(
            SELECT 1 FROM team_statuses s JOIN teams t ON t.id=s.team_id
            WHERE s.id=NEW.target_status_id AND t.workspace_id=wid AND NOT t.is_private
        ) THEN
            RAISE EXCEPTION 'GitHub automation requires a public-team status' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.issue_id IS NOT NULL AND NOT EXISTS(
        SELECT 1 FROM issues i JOIN teams t ON t.id=i.team_id
        WHERE i.id=NEW.issue_id AND i.workspace_id=wid AND NOT t.is_private
    ) THEN
        RAISE EXCEPTION 'GitHub activity requires a public issue' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION kuayle_private_project_status_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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

DO $$ DECLARE tab TEXT; BEGIN
 FOREACH tab IN ARRAY ARRAY['issues','comments','issue_history','projects','issue_templates','cycles','teams'] LOOP
  EXECUTE format('DROP TRIGGER IF EXISTS private_asset_refs_sync ON %I',tab);
 END LOOP;
END $$;
DROP FUNCTION IF EXISTS kuayle_private_asset_refs_sync();
DROP FUNCTION IF EXISTS kuayle_private_asset_source_valid(TEXT,UUID);
DROP FUNCTION IF EXISTS kuayle_private_asset_id_valid(UUID);
DROP FUNCTION IF EXISTS kuayle_private_issue_valid(UUID);
CREATE OR REPLACE VIEW private_asset_references AS
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
DROP FUNCTION IF EXISTS kuayle_private_owner_scope(TEXT,UUID);
DROP TABLE IF EXISTS private_asset_reference_index;

DROP FUNCTION IF EXISTS kuayle_team_access(UUID,UUID,UUID);
DROP FUNCTION IF EXISTS kuayle_workspace_access(UUID,UUID,BOOLEAN);
