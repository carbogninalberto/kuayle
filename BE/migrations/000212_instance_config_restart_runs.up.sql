-- Settings -> Instance configuration -> Apply and restart (KUA-311): each
-- time a sysadmin applies restart-only settings, one row records who asked,
-- when, and old -> new values of every variable (restart-editable variables
-- are never secrets, so the values are safe to keep), plus the overrides
-- before the change, which an automatic rollback restores. The overrides
-- themselves live in instance_config_overrides; the API merges the
-- restart-editable ones over its environment when it starts.
--
-- status: restarting (saved, the API is about to exit), applying (the new
-- API process started and has not reported healthy yet), confirmed (it
-- reported healthy) or rolled_back (it did not within the timeout, kept
-- crashing, or its configuration was invalid; the previous overrides are
-- back).
CREATE TABLE instance_config_restart_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status TEXT NOT NULL CHECK (status IN ('restarting', 'applying', 'confirmed', 'rolled_back')),
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- [{"name": "...", "from": "...", "to": "..."}] effective values.
    changes JSONB NOT NULL,
    -- {"NAME": "value" | null}: the override of each changed variable before
    -- the run; null means there was none.
    previous JSONB NOT NULL,
    boot_attempts INTEGER NOT NULL DEFAULT 0,
    applied_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    failure TEXT NOT NULL DEFAULT ''
);

-- One run at a time.
CREATE UNIQUE INDEX instance_config_restart_runs_active
    ON instance_config_restart_runs ((true))
    WHERE status IN ('restarting', 'applying');
CREATE INDEX instance_config_restart_runs_requested_at ON instance_config_restart_runs (requested_at DESC);
