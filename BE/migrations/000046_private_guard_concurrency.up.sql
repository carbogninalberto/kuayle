-- VOLATILE functions deliberately take a fresh READ COMMITTED snapshot inside
-- each access check, including after a statement trigger waited for classification.
-- An outer SQL predicate alone retains the snapshot from before that wait.
CREATE FUNCTION kuayle_workspace_access(wid UUID, actor UUID, admin_only BOOLEAN)
RETURNS BOOLEAN LANGUAGE plpgsql VOLATILE AS $$
BEGIN
 RETURN EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=wid AND m.user_id=actor
  AND (NOT admin_only OR m.role IN ('owner','admin')));
END;
$$;
CREATE FUNCTION kuayle_team_access(ident UUID, actor UUID, scope UUID)
RETURNS BOOLEAN LANGUAGE plpgsql VOLATILE AS $$
BEGIN
 RETURN EXISTS(
  SELECT 1 FROM teams t LEFT JOIN workspace_members m ON m.workspace_id=t.workspace_id AND m.user_id=actor
  WHERE t.id=ident AND (scope IS NULL OR t.workspace_id=scope)
   AND (actor IS NULL OR m.user_id IS NOT NULL)
   AND (NOT t.is_private OR (actor IS NOT NULL AND
     (m.role IN ('owner','admin') OR EXISTS(SELECT 1 FROM team_members tm WHERE tm.team_id=t.id AND tm.user_id=actor))))
 );
END;
$$;

-- Ordinary statements acquire a compatible graph barrier before tuple locks.
-- Classification changes take the exclusive side. Statement-level coverage of
-- application tables also covers FK cascades and junction mutations that precede
-- an issue update in the same transaction. No external I/O holds this barrier.
CREATE FUNCTION kuayle_privacy_statement_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_ARGV[0]='exclusive' THEN
  IF NOT pg_try_advisory_xact_lock(610046,0) THEN
   -- An arbitrary SQL transaction may have already written rows under a shared
   -- barrier. Never wait while upgrading it; the caller must retry that transaction.
   IF EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory'
      AND classid=610046 AND objid=0 AND objsubid=2 AND mode='ShareLock' AND granted) THEN
    RAISE EXCEPTION 'Retry classification in a new transaction before other writes' USING ERRCODE='40001';
   END IF;
   PERFORM pg_advisory_xact_lock(610046,0);
  END IF;
 ELSE
  PERFORM pg_advisory_xact_lock_shared(610046,0);
 END IF;
 RETURN NULL;
END;
$$;
DO $$ DECLARE tab TEXT; BEGIN
 FOR tab IN SELECT tablename FROM pg_tables WHERE schemaname=current_schema() AND tablename<>'schema_migrations' LOOP
  EXECUTE format('CREATE TRIGGER privacy_statement_shared BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard(''shared'')',tab);
 END LOOP;
END $$;
-- Names put exclusivity before the shared trigger on a classification statement.
CREATE TRIGGER privacy_statement_exclusive BEFORE INSERT OR DELETE ON teams
 FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('exclusive');
CREATE TRIGGER privacy_statement_classification BEFORE UPDATE OF is_private ON teams
 FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('exclusive');
CREATE TRIGGER privacy_statement_delete BEFORE DELETE ON workspaces
 FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('exclusive');
CREATE TRIGGER privacy_statement_delete BEFORE DELETE ON users
 FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('exclusive');

CREATE OR REPLACE FUNCTION kuayle_asset_scope_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_WHEN='AFTER' THEN
        IF NOT kuayle_private_asset_id_valid(NEW.id) THEN
            RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    NULL; -- acquired before tuple locks by the statement trigger
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
    NULL; -- acquired before tuple locks by the statement trigger
    IF TG_TABLE_NAME='teams' THEN
        IF (TG_OP='INSERT' AND NEW.is_private) OR (TG_OP='UPDATE' AND NEW.is_private IS DISTINCT FROM OLD.is_private) THEN
            -- API callers acquire this BEFORE entering the exclusive graph barrier.
            -- A raw SQL writer must not invert that order and wait on publication.
            IF NOT pg_try_advisory_xact_lock(hashtextextended(wid::TEXT,63)) THEN
                RAISE EXCEPTION 'Publication in progress; retry visibility in a new transaction' USING ERRCODE='40001';
            END IF;
        END IF;
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

-- Asset references are derived data. Ownership is resolved from the live owner,
-- not copied into this table (a concurrent public move cannot leave stale scope).
CREATE TABLE private_asset_reference_index (
 source_table TEXT NOT NULL, source_id UUID NOT NULL,
 owner_table TEXT NOT NULL, owner_id UUID NOT NULL, asset_id UUID NOT NULL,
 PRIMARY KEY(source_table,source_id,asset_id)
);
CREATE INDEX private_asset_reference_asset ON private_asset_reference_index(asset_id);
CREATE INDEX private_asset_reference_owner ON private_asset_reference_index(owner_table,owner_id);
CREATE TRIGGER privacy_statement_shared BEFORE INSERT OR UPDATE OR DELETE ON private_asset_reference_index
 FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('shared');
CREATE FUNCTION kuayle_private_owner_scope(kind TEXT, ident UUID)
RETURNS TABLE(workspace_id UUID,team_id UUID) LANGUAGE SQL VOLATILE AS $$
SELECT o.workspace_id,o.team_id FROM issues o  WHERE kind='issues' AND o.id=ident
UNION ALL
SELECT o.workspace_id,o.team_id FROM projects o  WHERE kind='projects' AND o.id=ident
UNION ALL
SELECT o.workspace_id,o.team_id FROM issue_templates o  WHERE kind='issue_templates' AND o.id=ident
UNION ALL
SELECT t.workspace_id,o.team_id FROM cycles o JOIN teams t ON t.id=o.team_id WHERE kind='cycles' AND o.id=ident
UNION ALL
SELECT o.workspace_id,o.id FROM teams o  WHERE kind='teams' AND o.id=ident;
$$;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'issues',id,'issues',id,(match[1])::UUID FROM issues CROSS JOIN LATERAL regexp_matches(COALESCE(description,''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'comments',id,'issues',issue_id,(match[1])::UUID FROM comments CROSS JOIN LATERAL regexp_matches(COALESCE(body,''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'issue_history',id,'issues',issue_id,(match[1])::UUID FROM issue_history CROSS JOIN LATERAL regexp_matches(COALESCE(CONCAT(old_value,' ',new_value),''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'projects',id,'projects',id,(match[1])::UUID FROM projects CROSS JOIN LATERAL regexp_matches(COALESCE(description,''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'issue_templates',id,'issue_templates',id,(match[1])::UUID FROM issue_templates CROSS JOIN LATERAL regexp_matches(COALESCE(description,''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'cycles',id,'cycles',id,(match[1])::UUID FROM cycles CROSS JOIN LATERAL regexp_matches(COALESCE(CONCAT(description,' ',goals,' ',retrospective),''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
INSERT INTO private_asset_reference_index SELECT DISTINCT 'teams',id,'teams',id,(match[1])::UUID FROM teams CROSS JOIN LATERAL regexp_matches(COALESCE(CONCAT(description,' ',issue_copy_prompt),''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
CREATE OR REPLACE VIEW private_asset_references AS
SELECT o.workspace_id AS workspace_id,o.team_id AS team_id,r.asset_id FROM private_asset_reference_index r JOIN issues o ON r.owner_table='issues' AND o.id=r.owner_id
UNION
SELECT o.workspace_id AS workspace_id,o.team_id AS team_id,r.asset_id FROM private_asset_reference_index r JOIN projects o ON r.owner_table='projects' AND o.id=r.owner_id
UNION
SELECT o.workspace_id AS workspace_id,o.team_id AS team_id,r.asset_id FROM private_asset_reference_index r JOIN issue_templates o ON r.owner_table='issue_templates' AND o.id=r.owner_id
UNION
SELECT t.workspace_id AS workspace_id,o.team_id AS team_id,r.asset_id FROM private_asset_reference_index r JOIN cycles o ON r.owner_table='cycles' AND o.id=r.owner_id JOIN teams t ON t.id=o.team_id
UNION
SELECT o.workspace_id AS workspace_id,o.id AS team_id,r.asset_id FROM private_asset_reference_index r JOIN teams o ON r.owner_table='teams' AND o.id=r.owner_id ;
CREATE FUNCTION kuayle_private_asset_source_valid(kind TEXT,ident UUID) RETURNS BOOLEAN LANGUAGE SQL VOLATILE AS $$
SELECT NOT EXISTS (
 SELECT 1 FROM private_asset_reference_index r
 CROSS JOIN LATERAL kuayle_private_owner_scope(r.owner_table,r.owner_id) scope
 JOIN assets a ON a.id=r.asset_id
 LEFT JOIN teams source ON source.id=scope.team_id
 LEFT JOIN teams owner ON owner.id=a.team_id
 WHERE r.source_table=kind AND r.source_id=ident
 AND (COALESCE(source.is_private,FALSE) OR COALESCE(owner.is_private,FALSE))
 AND (scope.team_id IS DISTINCT FROM a.team_id OR scope.workspace_id<>a.workspace_id)
);
$$;
CREATE FUNCTION kuayle_private_asset_id_valid(ident UUID) RETURNS BOOLEAN LANGUAGE SQL VOLATILE AS $$
SELECT NOT EXISTS (
 SELECT 1 FROM private_asset_reference_index r
 CROSS JOIN LATERAL kuayle_private_owner_scope(r.owner_table,r.owner_id) scope
 JOIN assets a ON a.id=r.asset_id
 LEFT JOIN teams source ON source.id=scope.team_id
 LEFT JOIN teams owner ON owner.id=a.team_id
 WHERE r.asset_id=ident
 AND (COALESCE(source.is_private,FALSE) OR COALESCE(owner.is_private,FALSE))
 AND (scope.team_id IS DISTINCT FROM a.team_id OR scope.workspace_id<>a.workspace_id)
);
$$;
CREATE FUNCTION kuayle_private_asset_refs_sync() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE data JSONB; previous JSONB; body TEXT; old_body TEXT; owner_kind TEXT; owner_ident UUID;
BEGIN
 IF TG_OP='DELETE' THEN
  DELETE FROM private_asset_reference_index WHERE source_table=TG_TABLE_NAME AND source_id=OLD.id;
  RETURN OLD;
 END IF;
 data:=to_jsonb(NEW);
 IF TG_OP='UPDATE' THEN previous:=to_jsonb(OLD); END IF;
 owner_kind:=TG_TABLE_NAME; owner_ident:=NEW.id;
 IF TG_TABLE_NAME IN ('comments','issue_history') THEN
  owner_kind:='issues'; owner_ident:=(data->>'issue_id')::UUID;
 END IF;
 IF TG_TABLE_NAME='comments' THEN
  body:=data->>'body'; old_body:=previous->>'body';
 ELSIF TG_TABLE_NAME='issue_history' THEN
  body:=CONCAT(data->>'old_value',' ',data->>'new_value');
  old_body:=CONCAT(previous->>'old_value',' ',previous->>'new_value');
 ELSIF TG_TABLE_NAME='cycles' THEN
  body:=CONCAT(data->>'description',' ',data->>'goals',' ',data->>'retrospective');
  old_body:=CONCAT(previous->>'description',' ',previous->>'goals',' ',previous->>'retrospective');
 ELSIF TG_TABLE_NAME='teams' THEN
  body:=CONCAT(data->>'description',' ',data->>'issue_copy_prompt');
  old_body:=CONCAT(previous->>'description',' ',previous->>'issue_copy_prompt');
 ELSE
  body:=data->>'description'; old_body:=previous->>'description';
 END IF;
 IF TG_OP='UPDATE' AND body IS NOT DISTINCT FROM old_body
    AND data->>'issue_id' IS NOT DISTINCT FROM previous->>'issue_id' THEN
  RETURN NEW;
 END IF;
 DELETE FROM private_asset_reference_index WHERE source_table=TG_TABLE_NAME AND source_id=NEW.id;
 INSERT INTO private_asset_reference_index
 SELECT DISTINCT TG_TABLE_NAME,NEW.id,owner_kind,owner_ident,(match[1])::UUID
 FROM regexp_matches(COALESCE(body,''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
 IF NOT kuayle_private_asset_source_valid(TG_TABLE_NAME,NEW.id) THEN
  RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
DO $$ DECLARE tab TEXT; BEGIN
 FOREACH tab IN ARRAY ARRAY['issues','comments','issue_history','projects','issue_templates','cycles','teams'] LOOP
  EXECUTE format('CREATE TRIGGER private_asset_refs_sync AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION kuayle_private_asset_refs_sync()',tab);
 END LOOP;
END $$;
CREATE FUNCTION kuayle_private_issue_valid(ident UUID) RETURNS BOOLEAN LANGUAGE SQL VOLATILE AS $$
SELECT NOT EXISTS (
    SELECT 1 FROM issues i JOIN teams t ON t.id=i.team_id
    LEFT JOIN projects p ON p.id=i.project_id LEFT JOIN teams pt ON pt.id=p.team_id
    LEFT JOIN cycles c ON c.id=i.cycle_id LEFT JOIN teams ct ON ct.id=c.team_id
    LEFT JOIN team_statuses s ON s.id=i.status_id LEFT JOIN teams st ON st.id=s.team_id
    LEFT JOIN issues parent ON parent.id=i.parent_id LEFT JOIN teams parent_t ON parent_t.id=parent.team_id
    WHERE i.id=ident
    AND (
        (t.is_private AND i.workspace_id<>t.workspace_id)
        OR (i.project_id IS NOT NULL AND (t.is_private OR COALESCE(pt.is_private,FALSE)) AND (i.team_id IS DISTINCT FROM p.team_id OR i.workspace_id<>p.workspace_id))
        OR (i.cycle_id IS NOT NULL AND (t.is_private OR COALESCE(ct.is_private,FALSE)) AND i.team_id IS DISTINCT FROM c.team_id)
        OR (i.status_id IS NOT NULL AND (t.is_private OR COALESCE(st.is_private,FALSE)) AND i.team_id IS DISTINCT FROM s.team_id)
        OR (i.parent_id IS NOT NULL AND (t.is_private OR COALESCE(parent_t.is_private,FALSE)) AND i.team_id IS DISTINCT FROM parent.team_id)
    )
);
$$;

-- Only classification changes need the complete graph. Ordinary writes validate
-- their affected edges; unchanged descriptions never reparse attachment URLs.
CREATE OR REPLACE FUNCTION kuayle_private_boundary_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE data JSONB; previous JSONB; wid UUID; changed_scope BOOLEAN; valid BOOLEAN:=TRUE;
BEGIN
 IF TG_OP='DELETE' THEN data:=to_jsonb(OLD); ELSE data:=to_jsonb(NEW); END IF;
 IF TG_OP='UPDATE' THEN previous:=to_jsonb(OLD); END IF;
 IF TG_TABLE_NAME IN ('teams','issues','projects','issue_templates') THEN
  wid:=(data->>'workspace_id')::UUID;
 ELSIF TG_TABLE_NAME IN ('cycles','team_statuses') THEN
  SELECT workspace_id INTO wid FROM teams WHERE id=(data->>'team_id')::UUID;
 ELSE
  SELECT workspace_id INTO wid FROM issues WHERE id=(data->>'issue_id')::UUID;
 END IF;
 changed_scope:=TG_OP='UPDATE' AND
  (previous->>'team_id' IS DISTINCT FROM data->>'team_id'
   OR previous->>'workspace_id' IS DISTINCT FROM data->>'workspace_id');
 IF TG_WHEN='BEFORE' THEN
  IF TG_TABLE_NAME='teams' AND TG_OP='DELETE' AND (data->>'is_private')::BOOLEAN
   AND EXISTS(SELECT 1 FROM workspaces WHERE id=wid)
   AND (EXISTS(SELECT 1 FROM projects WHERE team_id=OLD.id)
        OR EXISTS(SELECT 1 FROM issue_templates WHERE team_id=OLD.id)) THEN
   RAISE EXCEPTION 'Delete private projects and templates before deleting their team' USING ERRCODE='23514';
  END IF;
  IF changed_scope AND TG_TABLE_NAME IN ('issues','projects','issue_templates','cycles','team_statuses')
   AND EXISTS(SELECT 1 FROM teams WHERE is_private AND id IN
     ((previous->>'team_id')::UUID,(data->>'team_id')::UUID)) THEN
   RAISE EXCEPTION 'Moving resources across a private-team boundary is not supported' USING ERRCODE='23514';
  END IF;
  IF changed_scope AND TG_TABLE_NAME='teams'
   AND ((previous->>'is_private')::BOOLEAN OR (data->>'is_private')::BOOLEAN) THEN
   RAISE EXCEPTION 'Moving private teams between workspaces is not supported' USING ERRCODE='23514';
  END IF;
 ELSIF TG_TABLE_NAME='issues' THEN
  IF TG_OP='INSERT' OR changed_scope OR
    previous->>'project_id' IS DISTINCT FROM data->>'project_id' OR
    previous->>'cycle_id' IS DISTINCT FROM data->>'cycle_id' OR
    previous->>'status_id' IS DISTINCT FROM data->>'status_id' OR
    previous->>'parent_id' IS DISTINCT FROM data->>'parent_id' THEN
   valid:=kuayle_private_issue_valid(NEW.id);
  END IF;
 ELSIF TG_TABLE_NAME='issue_relations' THEN
  SELECT NOT EXISTS(
   SELECT 1 FROM issues a JOIN issues b ON b.id=NEW.related_issue_id
   JOIN teams ta ON ta.id=a.team_id JOIN teams tb ON tb.id=b.team_id
   WHERE a.id=NEW.issue_id AND (ta.is_private OR tb.is_private) AND a.team_id<>b.team_id
  ) INTO valid;
 ELSIF TG_TABLE_NAME='teams' THEN
  IF (TG_OP='INSERT' AND (data->>'is_private')::BOOLEAN) OR
     (TG_OP='UPDATE' AND (previous->>'is_private' IS DISTINCT FROM data->>'is_private' OR changed_scope)) THEN
   valid:=kuayle_private_edges_valid(wid);
  END IF;
 ELSIF changed_scope AND TG_TABLE_NAME IN ('projects','cycles','team_statuses','issue_templates') THEN
  valid:=kuayle_private_edges_valid(wid);
  IF previous->>'workspace_id' IS DISTINCT FROM data->>'workspace_id' AND previous->>'workspace_id' IS NOT NULL THEN
   valid:=valid AND kuayle_private_edges_valid((previous->>'workspace_id')::UUID);
  END IF;
 END IF;
 IF NOT valid THEN
  RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514',
   HINT='Remove cross-team relationships before making the team private.';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION kuayle_private_project_status_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_WHEN='AFTER' AND EXISTS(
  SELECT 1 FROM projects p LEFT JOIN teams pt ON pt.id=p.team_id
  JOIN team_statuses s ON s.id=NEW.status_id JOIN teams st ON st.id=s.team_id
  WHERE p.id=NEW.project_id AND (COALESCE(pt.is_private,FALSE) OR st.is_private)
   AND (p.team_id IS DISTINCT FROM s.team_id OR p.workspace_id<>st.workspace_id)
 ) THEN
  RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
-- References may precede their asset row. Serialize asset creation with graph
-- writers so concurrent missing-asset references cannot both evade validation.
CREATE TRIGGER privacy_statement_exclusive BEFORE INSERT ON assets
 FOR EACH STATEMENT EXECUTE FUNCTION kuayle_privacy_statement_guard('exclusive');
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM workspace_privacy WHERE NOT kuayle_private_edges_valid(workspace_id)) THEN
  RAISE EXCEPTION 'Resolve private attachment references before migration';
 END IF;
END $$;
