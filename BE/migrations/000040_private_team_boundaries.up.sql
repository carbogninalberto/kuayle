-- Serialize privacy/relationship changes per workspace. The AFTER check sees the
-- final row, including public-to-private transitions and FK-driven updates.
CREATE FUNCTION kuayle_private_edges_valid(wid UUID) RETURNS BOOLEAN
LANGUAGE SQL VOLATILE AS $$
SELECT NOT EXISTS (
    SELECT 1 FROM issues i JOIN teams t ON t.id=i.team_id
    LEFT JOIN projects p ON p.id=i.project_id LEFT JOIN teams pt ON pt.id=p.team_id
    LEFT JOIN cycles c ON c.id=i.cycle_id LEFT JOIN teams ct ON ct.id=c.team_id
    LEFT JOIN team_statuses s ON s.id=i.status_id LEFT JOIN teams st ON st.id=s.team_id
    LEFT JOIN issues parent ON parent.id=i.parent_id LEFT JOIN teams parent_t ON parent_t.id=parent.team_id
    WHERE (i.workspace_id=wid OR t.workspace_id=wid OR pt.workspace_id=wid OR ct.workspace_id=wid OR st.workspace_id=wid OR parent_t.workspace_id=wid)
    AND (
        (t.is_private AND i.workspace_id<>t.workspace_id)
        OR (i.project_id IS NOT NULL AND (t.is_private OR COALESCE(pt.is_private,FALSE)) AND (i.team_id IS DISTINCT FROM p.team_id OR i.workspace_id<>p.workspace_id))
        OR (i.cycle_id IS NOT NULL AND (t.is_private OR COALESCE(ct.is_private,FALSE)) AND i.team_id IS DISTINCT FROM c.team_id)
        OR (i.status_id IS NOT NULL AND (t.is_private OR COALESCE(st.is_private,FALSE)) AND i.team_id IS DISTINCT FROM s.team_id)
        OR (i.parent_id IS NOT NULL AND (t.is_private OR COALESCE(parent_t.is_private,FALSE)) AND i.team_id IS DISTINCT FROM parent.team_id)
    )
    UNION ALL
    SELECT 1 FROM issue_relations r JOIN issues a ON a.id=r.issue_id JOIN issues b ON b.id=r.related_issue_id
    JOIN teams ta ON ta.id=a.team_id JOIN teams tb ON tb.id=b.team_id
    WHERE (a.workspace_id=wid OR b.workspace_id=wid) AND (ta.is_private OR tb.is_private) AND a.team_id<>b.team_id
);
$$;

CREATE FUNCTION kuayle_private_boundary_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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

DO $$ DECLARE tab TEXT; BEGIN
    FOREACH tab IN ARRAY ARRAY['teams','issues','projects','cycles','team_statuses','issue_relations','issue_templates'] LOOP
        EXECUTE format('CREATE TRIGGER private_boundary_lock BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION kuayle_private_boundary_guard()',tab);
        EXECUTE format('CREATE TRIGGER private_boundary_check AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION kuayle_private_boundary_guard()',tab);
    END LOOP;
END $$;
