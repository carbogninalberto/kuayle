-- Rolling back drops the history of applied restarts. The overrides it applied
-- stay in instance_config_overrides, but a release without the feature does
-- not read the restart-only ones, so they stop applying.
DROP TABLE IF EXISTS instance_config_restart_runs;
