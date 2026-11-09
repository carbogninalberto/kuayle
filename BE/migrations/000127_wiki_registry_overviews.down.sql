-- Irreversible for data: rolling back deletes every registry overview and schedule.
DROP INDEX IF EXISTS wiki_settings_overview_due_idx;
ALTER TABLE wiki_settings
    DROP COLUMN IF EXISTS overview_last_error,
    DROP COLUMN IF EXISTS overview_last_run_at,
    DROP COLUMN IF EXISTS overview_next_run_at,
    DROP COLUMN IF EXISTS overview_actor,
    DROP COLUMN IF EXISTS overview_period_days,
    DROP COLUMN IF EXISTS overview_time,
    DROP COLUMN IF EXISTS overview_weekday,
    DROP COLUMN IF EXISTS overview_frequency,
    DROP COLUMN IF EXISTS overview_enabled;
DROP TABLE IF EXISTS wiki_registry_overviews;
