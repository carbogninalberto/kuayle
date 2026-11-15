-- Issue soft delete and optimistic versions (KUA-51).
--
-- The table becomes issue_records and "issues" becomes a simple view over the
-- rows that are not deleted. The view is automatically updatable, so existing
-- INSERT/UPDATE/DELETE/RETURNING/FOR UPDATE statements keep working and every
-- read hides deleted issues without a per-query filter. Code that must see
-- deleted rows (restore, purge, issue numbering, team key renames) names
-- issue_records explicitly. Foreign keys, indexes, constraints and triggers
-- follow the renamed table. New columns must be added to issue_records and
-- the view recreated with them.

ALTER TABLE issues RENAME TO issue_records;

ALTER TABLE issue_records
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN deleted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1;

CREATE INDEX idx_issue_records_deleted_at ON issue_records(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE VIEW issues AS
SELECT id, workspace_id, team_id, project_id, cycle_id, number, identifier_text,
       title, description, status, priority, creator_id, assignee_id, parent_id,
       due_date, sort_order, created_at, updated_at, triaged, status_id,
       started_at, completed_at, cancelled_at, triaged_at, version
FROM issue_records
WHERE deleted_at IS NULL;

-- Every write to an issue row advances its version; undo compares it.
CREATE FUNCTION kuayle_issue_version_bump() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER issue_records_version BEFORE UPDATE ON issue_records
    FOR EACH ROW EXECUTE FUNCTION kuayle_issue_version_bump();

-- Trigger functions shared with other tables branch on TG_TABLE_NAME and
-- store it as the logical owner name ('issues'); keep that name for the
-- renamed table. Integrity checks that a restore could later violate
-- (private-team edges, parent cycles, asset owners) read every row,
-- including deleted ones.

CREATE OR REPLACE FUNCTION public.kuayle_private_boundary_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE tbl TEXT:=CASE TG_TABLE_NAME WHEN 'issue_records' THEN 'issues' ELSE TG_TABLE_NAME END; data JSONB; previous JSONB; wid UUID; changed_scope BOOLEAN; valid BOOLEAN:=TRUE;
BEGIN
 IF TG_OP='DELETE' THEN data:=to_jsonb(OLD); ELSE data:=to_jsonb(NEW); END IF;
 IF TG_OP='UPDATE' THEN previous:=to_jsonb(OLD); END IF;
 IF tbl IN ('teams','issues','projects','issue_templates') THEN
  wid:=(data->>'workspace_id')::UUID;
 ELSIF tbl IN ('cycles','team_statuses') THEN
  SELECT workspace_id INTO wid FROM teams WHERE id=(data->>'team_id')::UUID;
 ELSE
  SELECT workspace_id INTO wid FROM issue_records WHERE id=(data->>'issue_id')::UUID;
 END IF;
 changed_scope:=TG_OP='UPDATE' AND
  (previous->>'team_id' IS DISTINCT FROM data->>'team_id'
   OR previous->>'workspace_id' IS DISTINCT FROM data->>'workspace_id');
 IF TG_WHEN='BEFORE' THEN
  IF tbl='teams' AND TG_OP='DELETE' AND (data->>'is_private')::BOOLEAN
   AND EXISTS(SELECT 1 FROM workspaces WHERE id=wid)
   AND (EXISTS(SELECT 1 FROM projects WHERE team_id=OLD.id)
        OR EXISTS(SELECT 1 FROM issue_templates WHERE team_id=OLD.id)) THEN
   RAISE EXCEPTION 'Delete private projects and templates before deleting their team' USING ERRCODE='23514';
  END IF;
  IF changed_scope AND tbl IN ('issues','projects','issue_templates','cycles','team_statuses')
   AND EXISTS(SELECT 1 FROM teams WHERE is_private AND id IN
     ((previous->>'team_id')::UUID,(data->>'team_id')::UUID)) THEN
   RAISE EXCEPTION 'Moving resources across a private-team boundary is not supported' USING ERRCODE='23514';
  END IF;
  IF changed_scope AND tbl='teams'
   AND ((previous->>'is_private')::BOOLEAN OR (data->>'is_private')::BOOLEAN) THEN
   RAISE EXCEPTION 'Moving private teams between workspaces is not supported' USING ERRCODE='23514';
  END IF;
 ELSIF tbl='issues' THEN
  IF TG_OP='INSERT' OR changed_scope OR
    previous->>'project_id' IS DISTINCT FROM data->>'project_id' OR
    previous->>'cycle_id' IS DISTINCT FROM data->>'cycle_id' OR
    previous->>'status_id' IS DISTINCT FROM data->>'status_id' OR
    previous->>'parent_id' IS DISTINCT FROM data->>'parent_id' THEN
   valid:=kuayle_private_issue_valid(NEW.id);
  END IF;
 ELSIF tbl='issue_relations' THEN
  SELECT NOT EXISTS(
   SELECT 1 FROM issue_records a JOIN issue_records b ON b.id=NEW.related_issue_id
   JOIN teams ta ON ta.id=a.team_id JOIN teams tb ON tb.id=b.team_id
   WHERE a.id=NEW.issue_id AND (ta.is_private OR tb.is_private) AND a.team_id<>b.team_id
  ) INTO valid;
 ELSIF tbl='teams' THEN
  IF (TG_OP='INSERT' AND (data->>'is_private')::BOOLEAN) OR
     (TG_OP='UPDATE' AND (previous->>'is_private' IS DISTINCT FROM data->>'is_private' OR changed_scope)) THEN
   valid:=kuayle_private_edges_valid(wid);
  END IF;
 ELSIF changed_scope AND tbl IN ('projects','cycles','team_statuses','issue_templates') THEN
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
$function$
;

CREATE OR REPLACE FUNCTION public.kuayle_private_asset_refs_sync()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE tbl TEXT:=CASE TG_TABLE_NAME WHEN 'issue_records' THEN 'issues' ELSE TG_TABLE_NAME END; data JSONB; previous JSONB; body TEXT; old_body TEXT; owner_kind TEXT; owner_ident UUID;
BEGIN
 IF TG_OP='DELETE' THEN
  DELETE FROM private_asset_reference_index WHERE source_table=tbl AND source_id=OLD.id;
  RETURN OLD;
 END IF;
 data:=to_jsonb(NEW);
 IF TG_OP='UPDATE' THEN previous:=to_jsonb(OLD); END IF;
 owner_kind:=tbl; owner_ident:=NEW.id;
 IF tbl IN ('comments','issue_history') THEN
  owner_kind:='issues'; owner_ident:=(data->>'issue_id')::UUID;
 END IF;
 IF tbl='comments' THEN
  body:=data->>'body'; old_body:=previous->>'body';
 ELSIF tbl='issue_history' THEN
  body:=CONCAT(data->>'old_value',' ',data->>'new_value');
  old_body:=CONCAT(previous->>'old_value',' ',previous->>'new_value');
 ELSIF tbl='cycles' THEN
  body:=CONCAT(data->>'description',' ',data->>'goals',' ',data->>'retrospective');
  old_body:=CONCAT(previous->>'description',' ',previous->>'goals',' ',previous->>'retrospective');
 ELSIF tbl='teams' THEN
  body:=CONCAT(data->>'description',' ',data->>'issue_copy_prompt');
  old_body:=CONCAT(previous->>'description',' ',previous->>'issue_copy_prompt');
 ELSE
  body:=data->>'description'; old_body:=previous->>'description';
 END IF;
 IF TG_OP='UPDATE' AND body IS NOT DISTINCT FROM old_body
    AND data->>'issue_id' IS NOT DISTINCT FROM previous->>'issue_id' THEN
  RETURN NEW;
 END IF;
 DELETE FROM private_asset_reference_index WHERE source_table=tbl AND source_id=NEW.id;
 INSERT INTO private_asset_reference_index
 SELECT DISTINCT tbl,NEW.id,owner_kind,owner_ident,(match[1])::UUID
 FROM regexp_matches(COALESCE(body,''),'/assets/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})','g') match;
 IF NOT kuayle_private_asset_source_valid(tbl,NEW.id) THEN
  RAISE EXCEPTION 'Resource relationship crosses a private-team boundary' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$function$
;

CREATE OR REPLACE FUNCTION public.kuayle_private_issue_valid(ident uuid)
 RETURNS boolean
 LANGUAGE sql
AS $function$
SELECT NOT EXISTS (
    SELECT 1 FROM issue_records i JOIN teams t ON t.id=i.team_id
    LEFT JOIN projects p ON p.id=i.project_id LEFT JOIN teams pt ON pt.id=p.team_id
    LEFT JOIN cycles c ON c.id=i.cycle_id LEFT JOIN teams ct ON ct.id=c.team_id
    LEFT JOIN team_statuses s ON s.id=i.status_id LEFT JOIN teams st ON st.id=s.team_id
    LEFT JOIN issue_records parent ON parent.id=i.parent_id LEFT JOIN teams parent_t ON parent_t.id=parent.team_id
    WHERE i.id=ident
    AND (
        (t.is_private AND i.workspace_id<>t.workspace_id)
        OR (i.project_id IS NOT NULL AND (t.is_private OR COALESCE(pt.is_private,FALSE)) AND (i.team_id IS DISTINCT FROM p.team_id OR i.workspace_id<>p.workspace_id))
        OR (i.cycle_id IS NOT NULL AND (t.is_private OR COALESCE(ct.is_private,FALSE)) AND i.team_id IS DISTINCT FROM c.team_id)
        OR (i.status_id IS NOT NULL AND (t.is_private OR COALESCE(st.is_private,FALSE)) AND i.team_id IS DISTINCT FROM s.team_id)
        OR (i.parent_id IS NOT NULL AND (t.is_private OR COALESCE(parent_t.is_private,FALSE)) AND i.team_id IS DISTINCT FROM parent.team_id)
    )
);
$function$
;

CREATE OR REPLACE FUNCTION public.kuayle_private_resource_edges_valid(wid uuid)
 RETURNS boolean
 LANGUAGE sql
AS $function$
SELECT NOT EXISTS (
    SELECT 1 FROM issue_records i JOIN teams t ON t.id=i.team_id
    LEFT JOIN projects p ON p.id=i.project_id LEFT JOIN teams pt ON pt.id=p.team_id
    LEFT JOIN cycles c ON c.id=i.cycle_id LEFT JOIN teams ct ON ct.id=c.team_id
    LEFT JOIN team_statuses s ON s.id=i.status_id LEFT JOIN teams st ON st.id=s.team_id
    LEFT JOIN issue_records parent ON parent.id=i.parent_id LEFT JOIN teams parent_t ON parent_t.id=parent.team_id
    WHERE (i.workspace_id=wid OR t.workspace_id=wid OR pt.workspace_id=wid OR ct.workspace_id=wid OR st.workspace_id=wid OR parent_t.workspace_id=wid)
    AND (
        (t.is_private AND i.workspace_id<>t.workspace_id)
        OR (i.project_id IS NOT NULL AND (t.is_private OR COALESCE(pt.is_private,FALSE)) AND (i.team_id IS DISTINCT FROM p.team_id OR i.workspace_id<>p.workspace_id))
        OR (i.cycle_id IS NOT NULL AND (t.is_private OR COALESCE(ct.is_private,FALSE)) AND i.team_id IS DISTINCT FROM c.team_id)
        OR (i.status_id IS NOT NULL AND (t.is_private OR COALESCE(st.is_private,FALSE)) AND i.team_id IS DISTINCT FROM s.team_id)
        OR (i.parent_id IS NOT NULL AND (t.is_private OR COALESCE(parent_t.is_private,FALSE)) AND i.team_id IS DISTINCT FROM parent.team_id)
    )
    UNION ALL
    SELECT 1 FROM issue_relations r JOIN issue_records a ON a.id=r.issue_id JOIN issue_records b ON b.id=r.related_issue_id
    JOIN teams ta ON ta.id=a.team_id JOIN teams tb ON tb.id=b.team_id
    WHERE (a.workspace_id=wid OR b.workspace_id=wid) AND (ta.is_private OR tb.is_private) AND a.team_id<>b.team_id
);
$function$
;

CREATE OR REPLACE FUNCTION public.kuayle_private_github_cleanup()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.is_private THEN
        DELETE FROM github_commits WHERE issue_id IN (SELECT id FROM issue_records WHERE team_id=NEW.id);
        DELETE FROM github_branches WHERE issue_id IN (SELECT id FROM issue_records WHERE team_id=NEW.id);
        DELETE FROM github_pull_requests WHERE issue_id IN (SELECT id FROM issue_records WHERE team_id=NEW.id);
        DELETE FROM github_auto_transitions WHERE target_status_id IN (SELECT id FROM team_statuses WHERE team_id=NEW.id);
    END IF;
    RETURN NEW;
END;
$function$
;

CREATE OR REPLACE FUNCTION public.kuayle_private_owner_scope(kind text, ident uuid)
 RETURNS TABLE(workspace_id uuid, team_id uuid)
 LANGUAGE sql
AS $function$
SELECT o.workspace_id,o.team_id FROM issue_records o  WHERE kind='issues' AND o.id=ident
UNION ALL
SELECT o.workspace_id,o.team_id FROM projects o  WHERE kind='projects' AND o.id=ident
UNION ALL
SELECT o.workspace_id,o.team_id FROM issue_templates o  WHERE kind='issue_templates' AND o.id=ident
UNION ALL
SELECT t.workspace_id,o.team_id FROM cycles o JOIN teams t ON t.id=o.team_id WHERE kind='cycles' AND o.id=ident
UNION ALL
SELECT o.workspace_id,o.id FROM teams o  WHERE kind='teams' AND o.id=ident;
$function$
;

CREATE OR REPLACE FUNCTION public.prevent_issue_parent_cycle()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.parent_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.parent_id = NEW.id THEN
        RAISE EXCEPTION 'issue cannot be its own parent';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM issue_records parent
        WHERE parent.id = NEW.parent_id
          AND parent.workspace_id <> NEW.workspace_id
    ) THEN
        RAISE EXCEPTION 'issue parent must belong to the same workspace';
    END IF;
    IF EXISTS (
        WITH RECURSIVE ancestors AS (
            SELECT id, parent_id
            FROM issue_records
            WHERE id = NEW.parent_id
            UNION ALL
            SELECT i.id, i.parent_id
            FROM issue_records i
            INNER JOIN ancestors a ON i.id = a.parent_id
        )
        SELECT 1 FROM ancestors WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'issue parent cycle detected';
    END IF;
    RETURN NEW;
END;
$function$
;
