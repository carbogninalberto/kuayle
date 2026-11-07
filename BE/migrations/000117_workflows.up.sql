-- Configurable workflows (KUA-95). Agent profiles are reusable agents (role,
-- harness, model, effort, instructions); they carry no permissions, so every
-- step still runs under the workspace policy. A workflow is a named state
-- machine whose graphs are immutable versions. Defaults choose a workflow per
-- workspace, team or project; no default (or a NULL workflow) is the built-in
-- Standard workflow, which is today's fixed flow in code.
CREATE TABLE agent_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name)),
    role TEXT NOT NULL CHECK (role IN ('implementer', 'reviewer', 'tester', 'docs_writer', 'security_reviewer')),
    harness TEXT NOT NULL CHECK (char_length(harness) BETWEEN 1 AND 64),
    model TEXT NOT NULL DEFAULT '' CHECK (char_length(model) <= 128),
    effort TEXT NOT NULL DEFAULT '' CHECK (char_length(effort) <= 32),
    instructions TEXT NOT NULL DEFAULT '' CHECK (octet_length(instructions) <= 16384),
    -- Built-in profiles (implementer, reviewer) are created once per workspace.
    builtin_key TEXT CHECK (builtin_key IN ('implementer', 'reviewer')),
    archived_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX agent_profiles_name ON agent_profiles (workspace_id, lower(name)) WHERE archived_at IS NULL;
CREATE UNIQUE INDEX agent_profiles_builtin ON agent_profiles (workspace_id, builtin_key) WHERE builtin_key IS NOT NULL;

CREATE TABLE workflows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80 AND name = btrim(name)),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    latest_version INT NOT NULL DEFAULT 0 CHECK (latest_version >= 0),
    archived_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id)
);
CREATE UNIQUE INDEX workflows_name ON workflows (workspace_id, lower(name)) WHERE archived_at IS NULL;

-- Versions are immutable: a save inserts the next version, and tasks pin the
-- version they run.
CREATE TABLE workflow_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    version INT NOT NULL CHECK (version >= 1),
    graph JSONB NOT NULL CHECK (jsonb_typeof(graph) = 'object' AND octet_length(graph::text) <= 262144),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, version),
    FOREIGN KEY (workflow_id, workspace_id) REFERENCES workflows(id, workspace_id) ON DELETE CASCADE
);
CREATE FUNCTION workflow_versions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'workflow versions are immutable';
END $$;
CREATE TRIGGER workflow_versions_immutable BEFORE UPDATE ON workflow_versions
    FOR EACH ROW EXECUTE FUNCTION workflow_versions_immutable();

CREATE TABLE workflow_defaults (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('workspace', 'team', 'project')),
    scope_id UUID NOT NULL,
    -- NULL chooses the built-in Standard workflow for the scope.
    workflow_id UUID,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, scope, scope_id),
    FOREIGN KEY (workflow_id, workspace_id) REFERENCES workflows(id, workspace_id) ON DELETE CASCADE,
    CHECK (scope <> 'workspace' OR scope_id = workspace_id)
);

-- Every change to profiles, workflows and defaults, and every human decision
-- on a running workflow.
CREATE TABLE workflow_audit_events (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    entity TEXT NOT NULL CHECK (entity IN ('agent_profile', 'workflow', 'workflow_default', 'task_workflow')),
    entity_id UUID NOT NULL,
    action TEXT NOT NULL CHECK (char_length(action) BETWEEN 1 AND 40),
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX workflow_audit_events_entity ON workflow_audit_events (workspace_id, entity, entity_id, id);

-- A task's workflow: the pinned version (NULL runs the Standard fixed flow),
-- its current node, per-node visit counts, the iterations it started and
-- when the workflow started (for the time budget). workflow_wake_at asks the
-- worker to evaluate a waiting node (a review finished, CI reported, a
-- timeout is due).
ALTER TABLE agent_tasks
    ADD COLUMN workflow_version_id UUID REFERENCES workflow_versions(id),
    ADD COLUMN workflow_node TEXT,
    ADD COLUMN workflow_visits JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN workflow_iterations INT NOT NULL DEFAULT 0,
    ADD COLUMN workflow_started_at TIMESTAMPTZ,
    ADD COLUMN workflow_status TEXT CHECK (workflow_status IN ('running', 'waiting', 'paused', 'done', 'failed')),
    ADD COLUMN workflow_wake_at TIMESTAMPTZ,
    ADD COLUMN workflow_sequence INT NOT NULL DEFAULT 0,
    -- Budgets a person granted by continuing past the iteration or time
    -- budget, and when the current time budget started (NULL: at the start).
    ADD COLUMN workflow_budget_extensions INT NOT NULL DEFAULT 0 CHECK (workflow_budget_extensions >= 0),
    ADD COLUMN workflow_budget_from TIMESTAMPTZ,
    ADD CONSTRAINT agent_tasks_workflow_cursor CHECK (workflow_version_id IS NULL OR (workflow_node IS NOT NULL AND workflow_status IS NOT NULL));
CREATE INDEX agent_tasks_workflow_wake ON agent_tasks (workflow_wake_at) WHERE workflow_wake_at IS NOT NULL;

-- The path a task took: one row per node entered, with the transition that
-- led there and why it fired. Agent steps record the iteration (task
-- generation) they started; reviewer steps the review task they created.
CREATE TABLE agent_task_workflow_steps (
    task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    sequence INT NOT NULL CHECK (sequence >= 1),
    node_id TEXT NOT NULL CHECK (char_length(node_id) BETWEEN 1 AND 64),
    node_type TEXT NOT NULL,
    visit INT NOT NULL CHECK (visit >= 1),
    from_node TEXT,
    edge_id TEXT,
    condition TEXT,
    reason TEXT NOT NULL CHECK (char_length(reason) <= 2048),
    generation BIGINT,
    review_task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
    outcome TEXT,
    detail TEXT NOT NULL DEFAULT '' CHECK (octet_length(detail) <= 16384),
    -- A person's decision on an approval node or a reached limit.
    decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
    entered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    PRIMARY KEY (task_id, sequence)
);
CREATE INDEX agent_task_workflow_steps_review ON agent_task_workflow_steps (review_task_id) WHERE review_task_id IS NOT NULL;
