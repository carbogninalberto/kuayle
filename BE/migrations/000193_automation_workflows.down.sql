-- Older engines would otherwise run cron rules continuously or delegate to the
-- issue default instead of the selected workflow. Pause those rules on rollback.
UPDATE agent_automations SET enabled=false WHERE workflow_id IS NOT NULL OR cron <> '';
UPDATE agent_automation_runs r SET status='dismissed' FROM agent_automations a
WHERE a.id=r.automation_id AND (a.workflow_id IS NOT NULL OR a.cron <> '') AND r.status IN ('starting','pending_approval');
ALTER TABLE agent_automations DROP COLUMN schedule_error, DROP COLUMN last_checked_at, DROP COLUMN next_run_at,
    DROP COLUMN timezone, DROP COLUMN cron, DROP COLUMN pinned_version, DROP COLUMN workflow_id;
DROP INDEX agent_automation_runs_history;
ALTER TABLE agent_automation_runs DROP COLUMN attempts, DROP COLUMN replays,
    DROP COLUMN approved, DROP COLUMN lease_id, DROP COLUMN available_at;
