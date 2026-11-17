-- CI failure summaries stored in summary stay; only the commit and the
-- pending read are dropped.
DROP INDEX IF EXISTS agent_task_signals_details_pending;
ALTER TABLE agent_task_signals
    DROP COLUMN failure_details_pending,
    DROP COLUMN head_sha;
