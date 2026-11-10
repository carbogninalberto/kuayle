-- AI agents as issue assignees (KUA-107). An agent identity is an agent
-- profile seen as a member: the profile keeps the name, harness, model and
-- instructions, and these tables add what membership needs. issue_assignees
-- (people) is untouched.

-- Presentation of a profile as a member. A profile without a row uses defaults.
CREATE TABLE agent_identities (
    profile_id UUID PRIMARY KEY REFERENCES agent_profiles(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    avatar_color TEXT NOT NULL DEFAULT '' CHECK (avatar_color = '' OR avatar_color ~ '^#[0-9a-fA-F]{6}$'),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Administrators choose, per team, which agents can be assigned and mentioned.
-- No row means the agent is not assignable in that team.
CREATE TABLE agent_team_policies (
    profile_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    assignable BOOLEAN NOT NULL DEFAULT true,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_id, team_id)
);
CREATE INDEX agent_team_policies_team ON agent_team_policies (team_id) WHERE assignable;

-- A task started for an agent identity runs the built-in flow with that
-- profile's harness, model, instructions and skills.
ALTER TABLE agent_tasks ADD COLUMN agent_profile_id UUID REFERENCES agent_profiles(id) ON DELETE SET NULL;
CREATE INDEX agent_tasks_agent_profile ON agent_tasks (issue_id, agent_profile_id) WHERE agent_profile_id IS NOT NULL;

-- Agents assigned to an issue. assigned_by is the person the agent acts for;
-- request_id makes starting its task idempotent; task_id is that task.
CREATE TABLE issue_agent_assignees (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    assigned_by UUID REFERENCES users(id) ON DELETE SET NULL,
    request_id UUID NOT NULL DEFAULT gen_random_uuid(),
    task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
    error TEXT CHECK (octet_length(error) <= 1024),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, profile_id)
);
CREATE INDEX issue_agent_assignees_profile ON issue_agent_assignees (profile_id);
CREATE INDEX issue_agent_assignees_assigned_by ON issue_agent_assignees (workspace_id, assigned_by);

-- History rows written by a task that runs as an agent identity name it; the
-- row's user stays the person the agent acted for.
ALTER TABLE issue_history ADD COLUMN agent_profile_id UUID REFERENCES agent_profiles(id) ON DELETE SET NULL;
