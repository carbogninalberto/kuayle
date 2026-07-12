DROP TRIGGER IF EXISTS trg_issue_lifecycle_ts ON issues;
DROP TRIGGER IF EXISTS trg_issue_lifecycle_event ON issues;
DROP FUNCTION IF EXISTS issue_lifecycle_timestamps();
DROP FUNCTION IF EXISTS issue_lifecycle_log_event();

DROP TABLE IF EXISTS issue_lifecycle_events;

ALTER TABLE issues
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS triaged_at;
