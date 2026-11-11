-- The task notebook (KUA-134): one structured record per task, kept by
-- Kuayle outside the worktree. document is a domain.Notebook: the plan,
-- decisions, what is done, what remains, how to verify, findings, and every
-- instruction a person gave (kept apart from what agents wrote). The size
-- check is a last bound; the application bounds every section.
CREATE TABLE agent_task_notebooks (
    task_id      UUID PRIMARY KEY REFERENCES agent_tasks(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    document     JSONB NOT NULL,
    revision     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_task_notebooks_size CHECK (octet_length(document::text) <= 524288)
);

-- Who changed which sections at which step. author is 'agent' (an update the
-- agent of run_id wrote), 'kuayle' (what Kuayle recorded itself) or 'person'
-- (an instruction from user_id). The newest 200 changes of a task are kept.
CREATE TABLE agent_task_notebook_changes (
    task_id       UUID NOT NULL REFERENCES agent_task_notebooks(task_id) ON DELETE CASCADE,
    revision      INTEGER NOT NULL,
    author        TEXT NOT NULL CHECK (author IN ('agent', 'kuayle', 'person')),
    user_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    run_id        UUID,
    generation    BIGINT,
    step_sequence INTEGER,
    step_label    TEXT NOT NULL DEFAULT '',
    sections      TEXT[] NOT NULL DEFAULT '{}',
    summary       TEXT NOT NULL DEFAULT '' CHECK (octet_length(summary) <= 2048),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, revision)
);

-- Per attempt: whether it must leave a valid notebook update, how many it
-- left, why its last one was refused, whether its worktree was mounted
-- read-only (a planning step), and the prompt it received split by source.
ALTER TABLE dev_machine_agent_runs
    ADD COLUMN notebook_required BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN notebook_updates INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN notebook_problem TEXT,
    ADD COLUMN worktree_read_only BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN prompt_sources JSONB,
    ADD CONSTRAINT dev_machine_agent_runs_prompt_sources_size
        CHECK (prompt_sources IS NULL OR octet_length(prompt_sources::text) <= 1048576);
