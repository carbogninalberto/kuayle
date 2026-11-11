-- Drops every task notebook, its history and the recorded prompt sources.
-- This loss is not reversible.
ALTER TABLE dev_machine_agent_runs
    DROP CONSTRAINT IF EXISTS dev_machine_agent_runs_prompt_sources_size,
    DROP COLUMN IF EXISTS prompt_sources,
    DROP COLUMN IF EXISTS worktree_read_only,
    DROP COLUMN IF EXISTS notebook_problem,
    DROP COLUMN IF EXISTS notebook_updates,
    DROP COLUMN IF EXISTS notebook_required;
DROP TABLE IF EXISTS agent_task_notebook_changes;
DROP TABLE IF EXISTS agent_task_notebooks;
