-- Pre-owner_id databases could promote additional owners. An operator must
-- resolve these memberships explicitly before the unique index is installed.
DO $$
DECLARE affected text;
BEGIN
    SELECT string_agg(w.slug, ', ' ORDER BY w.slug) INTO affected
    FROM workspaces w WHERE w.id IN (
        SELECT workspace_id FROM workspace_members WHERE role='owner'
        GROUP BY workspace_id HAVING count(*) > 1
    );
    IF affected IS NOT NULL THEN
        RAISE EXCEPTION 'Multiple owner memberships in workspaces: %', affected
            USING HINT = 'Review the memberships against workspaces.owner_id and keep one owner before retrying migration 202.';
    END IF;
END $$;

-- The keys of the built-ins are the existing membership values. Custom keys
-- are independent of their editable display names.
CREATE TABLE workspace_roles (
    workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
    key text NOT NULL CHECK (key IN ('owner','admin','member','guest') OR
        key ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
    name varchar(100) NOT NULL CHECK (btrim(name) <> ''),
    description text NOT NULL DEFAULT '',
    permissions text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (workspace_id, key)
);
CREATE UNIQUE INDEX workspace_roles_name ON workspace_roles(workspace_id, lower(name));

CREATE VIEW builtin_workspace_roles AS
WITH grants AS (
    SELECT ARRAY['issues:read','comments:read','projects:read','cycles:read','labels:read',
        'teams:read','members:read','templates:read','views:read','analytics:read',
        'notifications:read','workspaces:read','account:read','assets:read','wiki:read',
        'account:write','notifications:write'] AS guest,
        ARRAY['issue:create','issue:update','issue:delete_own','project:manage','label:manage',
        'cycle:manage','view:manage','dev_machine:read','dev_machine:create','dev_machine:manage',
        'wiki:write','agent_tasks:read','workflows:read'] AS member,
        ARRAY['workspace:transfer','team:manage','issue:delete','member:invite','dev_machine:admin'] AS admin
)
SELECT role.* FROM grants CROSS JOIN LATERAL (VALUES
    ('owner','Owner','Owns the workspace.',guest || member || admin || ARRAY['workspace:manage']),
    ('admin','Admin','Administers teams and members.',guest || member || admin),
    ('member','Member','Contributes to workspace projects.',guest || member),
    ('guest','Guest','Reads shared workspace content.',guest)
) role(key,name,description,permissions);

INSERT INTO workspace_roles SELECT w.id, r.* FROM workspaces w CROSS JOIN builtin_workspace_roles r;
CREATE FUNCTION seed_workspace_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO workspace_roles SELECT NEW.id, r.* FROM builtin_workspace_roles r;
    RETURN NEW;
END $$;
CREATE TRIGGER seed_workspace_roles AFTER INSERT ON workspaces
    FOR EACH ROW EXECUTE FUNCTION seed_workspace_roles();

-- Legacy role-only policies see a guest for custom memberships. Permission
-- checks use the role key, scoped by workspace, including for existing users.
ALTER TABLE workspace_members
    ADD COLUMN assigned_role text CHECK (assigned_role IS NULL OR
        (role='guest' AND assigned_role <> 'owner')),
    ADD COLUMN role_key text GENERATED ALWAYS AS (coalesce(assigned_role,role)) STORED,
    ADD FOREIGN KEY (workspace_id,role_key) REFERENCES workspace_roles(workspace_id,key);
CREATE UNIQUE INDEX workspace_members_one_owner ON workspace_members(workspace_id) WHERE role='owner';

CREATE TABLE team_role_overrides (
    workspace_id uuid NOT NULL,
    team_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role_key text NOT NULL CHECK (role_key <> 'owner'),
    PRIMARY KEY (workspace_id,user_id,team_id),
    FOREIGN KEY (workspace_id,team_id) REFERENCES teams(workspace_id,id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id,user_id) REFERENCES workspace_members ON DELETE CASCADE,
    FOREIGN KEY (workspace_id,role_key) REFERENCES workspace_roles(workspace_id,key)
);
CREATE INDEX team_role_overrides_role ON team_role_overrides(workspace_id,role_key);
CREATE INDEX workspace_members_role_key ON workspace_members(workspace_id,role_key);

-- An override can narrow an administrator’s implicit private-team access only
-- in that team. A team role alone never confers private membership.
CREATE OR REPLACE FUNCTION kuayle_team_access(ident UUID, actor UUID, scope UUID)
RETURNS BOOLEAN LANGUAGE plpgsql VOLATILE AS $$
BEGIN
 RETURN EXISTS(
  SELECT 1 FROM teams t LEFT JOIN workspace_members m ON m.workspace_id=t.workspace_id AND m.user_id=actor
  WHERE t.id=ident AND (scope IS NULL OR t.workspace_id=scope)
   AND (actor IS NULL OR m.user_id IS NOT NULL)
   AND (NOT t.is_private OR (actor IS NOT NULL AND
     ((m.role_key IN ('owner','admin') AND NOT EXISTS (
       SELECT 1 FROM team_role_overrides o WHERE o.workspace_id=t.workspace_id AND o.user_id=actor
       AND o.team_id=t.id AND o.role_key NOT IN ('owner','admin'))) OR EXISTS(SELECT 1 FROM team_members tm WHERE tm.team_id=t.id AND tm.user_id=actor))))
 );
END;
$$;

-- Resource queries supply the stored team. Workspace-only operations pass NULL.
CREATE FUNCTION kuayle_role_allows(wid uuid, actor uuid, team uuid, permission text)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM workspace_members m
        LEFT JOIN team_role_overrides o ON o.workspace_id=m.workspace_id
            AND o.user_id=m.user_id AND o.team_id=team
        JOIN workspace_roles r ON r.workspace_id=m.workspace_id AND r.key=coalesce(o.role_key,m.role_key)
        WHERE m.workspace_id=wid AND m.user_id=actor AND permission=ANY(r.permissions)
    )
$$;

-- Workers and session-only features that still check role names must also
-- lose their broad member/admin privileges when a team exception is added.
CREATE FUNCTION sync_team_role_membership() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        UPDATE workspace_members SET assigned_role=role_key, role='guest'
            WHERE workspace_id=NEW.workspace_id AND user_id=NEW.user_id;
    ELSE
        UPDATE workspace_members m SET role=m.assigned_role, assigned_role=NULL
            WHERE m.workspace_id=OLD.workspace_id AND m.user_id=OLD.user_id
            AND m.assigned_role IN ('admin','member','guest')
            AND NOT EXISTS (SELECT 1 FROM team_role_overrides o
                WHERE o.workspace_id=m.workspace_id AND o.user_id=m.user_id);
    END IF;
    RETURN NULL;
END $$;
CREATE TRIGGER sync_team_role_membership AFTER INSERT OR DELETE ON team_role_overrides
    FOR EACH ROW EXECUTE FUNCTION sync_team_role_membership();

-- The gateway only needs the identities eligible for machine sessions.
CREATE VIEW dev_machine_role_access AS
SELECT m.workspace_id,m.user_id FROM workspace_members m
JOIN workspace_roles r ON (r.workspace_id,r.key)=(m.workspace_id,m.role_key)
WHERE r.permissions @> ARRAY['dev_machine:read','dev_machine:manage']
AND NOT EXISTS (
    SELECT 1 FROM team_role_overrides o JOIN workspace_roles t
        ON (t.workspace_id,t.key)=(o.workspace_id,o.role_key)
    WHERE o.workspace_id=m.workspace_id AND o.user_id=m.user_id
    AND NOT t.permissions @> ARRAY['dev_machine:read','dev_machine:manage']
);

CREATE FUNCTION kuayle_can_grant_role(wid uuid, actor_id uuid, requested text, scopes text[])
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT COALESCE((
        SELECT 'member:invite'=ANY(actor.permissions)
        AND target.permissions <@ actor.permissions AND (scopes IS NULL OR target.permissions <@ scopes)
        AND (m.role IN ('owner','admin') OR target.key NOT IN ('owner','admin','member'))
        AND NOT EXISTS (SELECT 1 FROM team_role_overrides o JOIN workspace_roles t ON (t.workspace_id,t.key)=(o.workspace_id,o.role_key)
          WHERE o.workspace_id=m.workspace_id AND o.user_id=m.user_id AND NOT (target.permissions <@ t.permissions AND 'member:invite'=ANY(t.permissions)))
        FROM workspace_members m JOIN workspace_roles actor ON (actor.workspace_id,actor.key)=(m.workspace_id,m.role_key)
        JOIN workspace_roles target ON target.workspace_id=m.workspace_id AND target.key=requested
        WHERE m.workspace_id=wid AND m.user_id=actor_id
    ),FALSE)
$$;
