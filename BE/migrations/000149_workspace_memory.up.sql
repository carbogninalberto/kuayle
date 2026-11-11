-- Workspace memory (KUA-134): short lessons per repository. Kuayle and agents
-- propose them; a workspace owner or admin approves, edits, retires or drops
-- each one, and only approved lessons reach an agent's prompt.
CREATE TABLE workspace_lessons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    repository_full_name TEXT NOT NULL CHECK (char_length(repository_full_name) BETWEEN 3 AND 512),
    -- Repositories are matched without regard to case, as GitHub does.
    repository_key TEXT NOT NULL CHECK (repository_key = lower(repository_full_name)),
    -- Set for a lesson that came from a private team's task: only people who
    -- can see that team see it, and only that team's tasks receive it. NULL is
    -- visible to every non-guest member and reaches every task on the repository.
    team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'proposed' CHECK (state IN ('proposed', 'approved', 'retired', 'dropped')),
    -- What agents see once the lesson is approved, as a person left it.
    text TEXT NOT NULL CHECK (char_length(text) BETWEEN 1 AND 400),
    -- What was proposed, kept as written.
    proposed_text TEXT NOT NULL CHECK (char_length(proposed_text) BETWEEN 1 AND 400),
    source_event TEXT NOT NULL CHECK (source_event IN ('task', 'correction', 'review', 'ci', 'manual')),
    -- Who wrote the proposed text: an agent, Kuayle (from CI or a GitHub
    -- review) or a workspace member.
    proposer TEXT NOT NULL CHECK (proposer IN ('agent', 'kuayle', 'person')),
    source_task_id UUID REFERENCES agent_tasks(id) ON DELETE SET NULL,
    proposed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    decided_by UUID REFERENCES users(id) ON DELETE SET NULL,
    decided_at TIMESTAMPTZ,
    -- Order of the approved lessons in a prompt, lowest first.
    position INTEGER NOT NULL DEFAULT 0,
    -- The same subject is proposed once per repository and scope, whatever a
    -- person decided about it.
    dedupe_key TEXT NOT NULL CHECK (char_length(dedupe_key) = 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (state = 'proposed' OR decided_at IS NOT NULL),
    CHECK (source_event <> 'manual' OR (proposer = 'person' AND source_task_id IS NULL))
);
CREATE UNIQUE INDEX workspace_lessons_dedupe ON workspace_lessons
    (workspace_id, repository_key, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid), dedupe_key);
CREATE INDEX workspace_lessons_list ON workspace_lessons (workspace_id, repository_key, state, position, created_at);
CREATE INDEX workspace_lessons_task ON workspace_lessons (source_task_id) WHERE source_task_id IS NOT NULL;

-- Every proposal and every decision on a lesson.
CREATE TABLE workspace_lesson_events (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    lesson_id UUID NOT NULL REFERENCES workspace_lessons(id) ON DELETE CASCADE,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL CHECK (action IN ('proposed', 'added', 'approved', 'edited', 'shared', 'retired', 'dropped', 'reordered')),
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX workspace_lesson_events_lesson ON workspace_lesson_events (lesson_id, id);
