-- Rollback drops only pending watches; task, conversation and notification
-- history stays untouched, and a watch that had not fired is simply lost.
DROP TABLE IF EXISTS agent_task_watchers;
