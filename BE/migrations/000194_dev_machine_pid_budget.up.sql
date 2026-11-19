ALTER TABLE dev_machines ALTER COLUMN pids_limit SET DEFAULT 4096;

UPDATE dev_machines SET pids_limit = 4096 WHERE pids_limit = 512;
