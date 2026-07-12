-- Issue lifecycle timestamps
ALTER TABLE issues
    ADD COLUMN started_at   TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN cancelled_at TIMESTAMPTZ,
    ADD COLUMN triaged_at   TIMESTAMPTZ;

-- Lifecycle event log for burnup / status transitions
CREATE TABLE issue_lifecycle_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id       UUID REFERENCES issues(id) ON DELETE SET NULL,
    from_status_id UUID,
    to_status_id   UUID,
    event_type     VARCHAR(20) NOT NULL CHECK (event_type IN ('created', 'status_changed')),
    from_category  VARCHAR(50),
    to_category    VARCHAR(50),
    workspace_id   UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id        UUID NOT NULL,
    project_id     UUID,
    cycle_id       UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_lifecycle_events_issue     ON issue_lifecycle_events(issue_id, created_at);
CREATE INDEX idx_lifecycle_events_created   ON issue_lifecycle_events(created_at);
CREATE INDEX idx_lifecycle_events_ws_cat_to ON issue_lifecycle_events(workspace_id, to_category, created_at);
CREATE INDEX idx_lifecycle_events_ws_cat_fr ON issue_lifecycle_events(workspace_id, from_category, created_at);
CREATE INDEX idx_lifecycle_events_team      ON issue_lifecycle_events(team_id, created_at);
CREATE INDEX idx_lifecycle_events_project   ON issue_lifecycle_events(project_id, created_at) WHERE project_id IS NOT NULL;
CREATE INDEX idx_lifecycle_events_cycle     ON issue_lifecycle_events(cycle_id, created_at) WHERE cycle_id IS NOT NULL;

-- Trigger function: sets lifecycle timestamps before the issue row is written.
CREATE OR REPLACE FUNCTION issue_lifecycle_timestamps()
RETURNS TRIGGER AS $$
DECLARE
    new_cat VARCHAR(50);
BEGIN
    IF NEW.status_id IS NOT NULL THEN
        SELECT category INTO new_cat FROM team_statuses WHERE id = NEW.status_id;
    END IF;
    IF new_cat IS NULL THEN
        new_cat := CASE NEW.status::text
            WHEN 'done'        THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'todo'        THEN 'unstarted'
            ELSE 'backlog'
        END;
    END IF;

    IF TG_OP = 'INSERT' THEN
        IF new_cat IN ('started', 'completed') AND NEW.started_at IS NULL THEN
            NEW.started_at = NEW.created_at;
        END IF;
        IF new_cat = 'completed' AND NEW.completed_at IS NULL THEN
            NEW.completed_at = NEW.created_at;
        END IF;
        IF new_cat = 'cancelled' AND NEW.cancelled_at IS NULL THEN
            NEW.cancelled_at = NEW.created_at;
        END IF;
        IF NEW.triaged = TRUE AND NEW.triaged_at IS NULL THEN
            NEW.triaged_at = NEW.created_at;
        END IF;
        RETURN NEW;
    END IF;

    -- UPDATE path
    IF NEW.started_at IS NULL AND new_cat IN ('started', 'completed') THEN
        NEW.started_at = NOW();
    END IF;

    IF NEW.completed_at IS NULL AND new_cat = 'completed' THEN
        NEW.completed_at = NOW();
    ELSIF new_cat <> 'completed' THEN
        NEW.completed_at = NULL;
    END IF;

    IF NEW.cancelled_at IS NULL AND new_cat = 'cancelled' THEN
        NEW.cancelled_at = NOW();
    ELSIF new_cat <> 'cancelled' THEN
        NEW.cancelled_at = NULL;
    END IF;

    IF OLD IS NULL OR (OLD.triaged = FALSE AND NEW.triaged = TRUE AND NEW.triaged_at IS NULL) THEN
        NEW.triaged_at = NOW();
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger function: logs status changes after the issue row exists.
CREATE OR REPLACE FUNCTION issue_lifecycle_log_event()
RETURNS TRIGGER AS $$
DECLARE
    old_cat VARCHAR(50);
    new_cat VARCHAR(50);
BEGIN
    IF NEW.status_id IS NOT NULL THEN
        SELECT category INTO new_cat FROM team_statuses WHERE id = NEW.status_id;
    END IF;
    IF new_cat IS NULL THEN
        new_cat := CASE NEW.status::text
            WHEN 'done'        THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'todo'        THEN 'unstarted'
            ELSE 'backlog'
        END;
    END IF;

    IF TG_OP = 'INSERT' THEN
        INSERT INTO issue_lifecycle_events
            (issue_id, from_status_id, to_status_id, event_type, from_category, to_category, created_at, workspace_id, team_id, project_id, cycle_id)
        VALUES (NEW.id, NULL, NEW.status_id, 'created', NULL, new_cat, NEW.created_at, NEW.workspace_id, NEW.team_id, NEW.project_id, NEW.cycle_id);
        RETURN NEW;
    END IF;

    -- UPDATE: old category
    IF OLD.status_id IS NOT NULL THEN
        SELECT category INTO old_cat FROM team_statuses WHERE id = OLD.status_id;
    END IF;
    IF old_cat IS NULL THEN
        old_cat := CASE OLD.status::text
            WHEN 'done'        THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'todo'        THEN 'unstarted'
            ELSE 'backlog'
        END;
    END IF;

    IF OLD.status_id IS DISTINCT FROM NEW.status_id OR OLD.status IS DISTINCT FROM NEW.status THEN
        INSERT INTO issue_lifecycle_events
            (issue_id, from_status_id, to_status_id, event_type, from_category, to_category, created_at, workspace_id, team_id, project_id, cycle_id)
        VALUES (NEW.id, OLD.status_id, NEW.status_id, 'status_changed', old_cat, new_cat, NOW(), NEW.workspace_id, NEW.team_id, NEW.project_id, NEW.cycle_id);
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_issue_lifecycle_ts ON issues;
CREATE TRIGGER trg_issue_lifecycle_ts
    BEFORE INSERT OR UPDATE ON issues
    FOR EACH ROW
    EXECUTE FUNCTION issue_lifecycle_timestamps();

DROP TRIGGER IF EXISTS trg_issue_lifecycle_event ON issues;
CREATE TRIGGER trg_issue_lifecycle_event
    AFTER INSERT OR UPDATE ON issues
    FOR EACH ROW
    EXECUTE FUNCTION issue_lifecycle_log_event();

-- Backfill existing issues timestamps
UPDATE issues i
SET
    started_at = sub.started,
    completed_at = sub.completed,
    cancelled_at = sub.cancelled,
    triaged_at = CASE WHEN i.triaged = TRUE THEN COALESCE(sub.triaged_ts, i.updated_at) END
FROM (
    SELECT
        i2.id,
        CASE
            WHEN COALESCE(ts.category,
                CASE i2.status::text
                    WHEN 'done' THEN 'completed' WHEN 'cancelled' THEN 'cancelled'
                    WHEN 'in_progress' THEN 'started' WHEN 'in_review' THEN 'started'
                    WHEN 'todo' THEN 'unstarted' ELSE 'backlog'
                END
            ) IN ('started', 'completed')
            THEN i2.created_at
            ELSE NULL
        END AS started,
        CASE
            WHEN COALESCE(ts.category,
                CASE i2.status::text
                    WHEN 'done' THEN 'completed' WHEN 'cancelled' THEN 'cancelled'
                    WHEN 'in_progress' THEN 'started' WHEN 'in_review' THEN 'started'
                    WHEN 'todo' THEN 'unstarted' ELSE 'backlog'
                END
            ) = 'completed'
            THEN i2.updated_at
            ELSE NULL
        END AS completed,
        CASE
            WHEN COALESCE(ts.category,
                CASE i2.status::text
                    WHEN 'done' THEN 'completed' WHEN 'cancelled' THEN 'cancelled'
                    WHEN 'in_progress' THEN 'started' WHEN 'in_review' THEN 'started'
                    WHEN 'todo' THEN 'unstarted' ELSE 'backlog'
                END
            ) = 'cancelled'
            THEN i2.updated_at
            ELSE NULL
        END AS cancelled,
        i2.updated_at AS triaged_ts
    FROM issues i2
    LEFT JOIN team_statuses ts ON ts.id = i2.status_id
) sub
WHERE i.id = sub.id
  AND (sub.started IS NOT NULL OR sub.completed IS NOT NULL OR sub.cancelled IS NOT NULL OR i.triaged = TRUE);

-- Add a creation baseline. Completed issues start in backlog here because their
-- synthetic completion transition is inserted below at completed_at.
INSERT INTO issue_lifecycle_events (issue_id, from_status_id, to_status_id, event_type, from_category, to_category, created_at, workspace_id, team_id, project_id, cycle_id)
SELECT
    i.id,
    NULL AS from_status_id,
    i.status_id,
    'created',
    NULL AS from_category,
    CASE
        WHEN COALESCE(ts.category, CASE WHEN i.status::text = 'done' THEN 'completed' ELSE 'backlog' END) = 'completed'
            THEN 'backlog'
        ELSE COALESCE(ts.category,
            CASE i.status::text
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'in_progress' THEN 'started' WHEN 'in_review' THEN 'started'
                WHEN 'todo' THEN 'unstarted' ELSE 'backlog'
            END
        )
    END,
    i.created_at,
    i.workspace_id,
    i.team_id,
    i.project_id,
    i.cycle_id
FROM issues i
LEFT JOIN team_statuses ts ON ts.id = i.status_id;

-- Synthetic completed event for each completed issue at its completed_at
INSERT INTO issue_lifecycle_events (issue_id, from_status_id, to_status_id, event_type, from_category, to_category, created_at, workspace_id, team_id, project_id, cycle_id)
SELECT
    i.id,
    NULL AS from_status_id,
    i.status_id,
    'status_changed',
    NULL AS from_category,
    'completed' AS to_category,
    i.completed_at,
    i.workspace_id,
    i.team_id,
    i.project_id,
    i.cycle_id
FROM issues i
WHERE i.completed_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM issue_lifecycle_events le WHERE le.issue_id = i.id AND le.to_category = 'completed');

-- Indexes for analytics queries
CREATE INDEX idx_issues_started_at   ON issues(started_at)   WHERE started_at IS NOT NULL;
CREATE INDEX idx_issues_completed_at ON issues(completed_at) WHERE completed_at IS NOT NULL;
CREATE INDEX idx_issues_cancelled_at ON issues(cancelled_at) WHERE cancelled_at IS NOT NULL;
CREATE INDEX idx_issues_triaged_at   ON issues(triaged_at)   WHERE triaged_at IS NOT NULL;
