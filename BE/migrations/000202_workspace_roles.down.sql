DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM team_role_overrides) OR
       EXISTS (SELECT 1 FROM workspace_roles WHERE key NOT IN ('owner','admin','member','guest')) THEN
        RAISE EXCEPTION 'Remove team overrides and custom roles before rolling back workspace roles';
    END IF;
END $$;
DROP FUNCTION kuayle_can_grant_role(uuid,uuid,text,text[]);
DROP VIEW dev_machine_role_access;
DROP FUNCTION kuayle_role_allows(uuid,uuid,uuid,text);
DROP TABLE team_role_overrides;
DROP FUNCTION sync_team_role_membership();
DROP INDEX workspace_members_one_owner;
ALTER TABLE workspace_members DROP COLUMN role_key, DROP COLUMN assigned_role;
DROP TRIGGER seed_workspace_roles ON workspaces;
DROP FUNCTION seed_workspace_roles();
DROP VIEW builtin_workspace_roles;
DROP TABLE workspace_roles;
