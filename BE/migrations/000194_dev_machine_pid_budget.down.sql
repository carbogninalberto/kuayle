-- Retain stored limits, including machines already using 4096 before the upgrade.
ALTER TABLE dev_machines ALTER COLUMN pids_limit SET DEFAULT 512;
