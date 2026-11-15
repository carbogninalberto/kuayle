-- Rolling back removes each machine's override; machines then follow their
-- workspace policy again. No machine or task data is lost.
ALTER TABLE dev_machines DROP COLUMN IF EXISTS max_tasks;
