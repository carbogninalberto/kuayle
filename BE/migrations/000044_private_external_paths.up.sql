-- Machine artifacts, images and historical prompts have no complete team
-- provenance. This increment requires clearing them before enabling privacy.
CREATE FUNCTION kuayle_private_external_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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
CREATE TRIGGER private_external_guard BEFORE INSERT OR UPDATE OF is_private ON teams
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_external_guard();
DO $$ DECLARE tab TEXT; BEGIN
    FOREACH tab IN ARRAY ARRAY['dev_machines','dev_machine_environments','dev_machine_scope_settings','dev_machine_workspace_policies','github_auto_transitions','github_pull_requests','github_branches','github_commits'] LOOP
        EXECUTE format('CREATE TRIGGER private_external_guard BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION kuayle_private_external_guard()',tab);
    END LOOP;
    IF EXISTS(SELECT 1 FROM workspace_privacy p WHERE
        EXISTS(SELECT 1 FROM dev_machines WHERE workspace_id=p.workspace_id)
        OR EXISTS(SELECT 1 FROM dev_machine_environments WHERE workspace_id=p.workspace_id)
        OR EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE workspace_id=p.workspace_id)) THEN
        RAISE EXCEPTION 'Remove development artifacts from private-team workspaces before migration';
    END IF;
END $$;

CREATE FUNCTION kuayle_private_github_cleanup() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.is_private THEN
        DELETE FROM github_commits WHERE issue_id IN (SELECT id FROM issues WHERE team_id=NEW.id);
        DELETE FROM github_branches WHERE issue_id IN (SELECT id FROM issues WHERE team_id=NEW.id);
        DELETE FROM github_pull_requests WHERE issue_id IN (SELECT id FROM issues WHERE team_id=NEW.id);
        DELETE FROM github_auto_transitions WHERE target_status_id IN (SELECT id FROM team_statuses WHERE team_id=NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER private_github_cleanup AFTER INSERT OR UPDATE OF is_private ON teams
    FOR EACH ROW EXECUTE FUNCTION kuayle_private_github_cleanup();
DELETE FROM github_commits WHERE issue_id IN (SELECT i.id FROM issues i JOIN teams t ON t.id=i.team_id WHERE t.is_private);
DELETE FROM github_branches WHERE issue_id IN (SELECT i.id FROM issues i JOIN teams t ON t.id=i.team_id WHERE t.is_private);
DELETE FROM github_pull_requests WHERE issue_id IN (SELECT i.id FROM issues i JOIN teams t ON t.id=i.team_id WHERE t.is_private);
DELETE FROM github_auto_transitions WHERE target_status_id IN (SELECT s.id FROM team_statuses s JOIN teams t ON t.id=s.team_id WHERE t.is_private);
