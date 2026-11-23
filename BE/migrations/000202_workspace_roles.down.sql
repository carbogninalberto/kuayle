DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM team_role_overrides) OR
       EXISTS (SELECT 1 FROM workspace_roles WHERE key NOT IN ('owner','admin','member','guest')) THEN
        RAISE EXCEPTION 'Remove team overrides and custom roles before rolling back workspace roles';
    END IF;
END $$;
DROP FUNCTION kuayle_can_grant_role(uuid,uuid,text,text[]);
DROP VIEW dev_machine_role_access;
DROP FUNCTION kuayle_role_allows(uuid,uuid,uuid,text);
CREATE OR REPLACE FUNCTION kuayle_team_access(ident UUID, actor UUID, scope UUID)
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
DROP TABLE team_role_overrides;
DROP FUNCTION sync_team_role_membership();
DROP INDEX workspace_members_one_owner;
ALTER TABLE workspace_members DROP COLUMN role_key, DROP COLUMN assigned_role;
DROP TRIGGER seed_workspace_roles ON workspaces;
DROP FUNCTION seed_workspace_roles();
DROP VIEW builtin_workspace_roles;
DROP TABLE workspace_roles;
