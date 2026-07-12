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
    old_cat VARCHAR(50);
    new_cat VARCHAR(50);
    status_changed BOOLEAN;
BEGIN
    IF NEW.status_id IS NOT NULL THEN
        SELECT category INTO new_cat FROM team_statuses WHERE id = NEW.status_id;
    END IF;
    IF new_cat IS NULL THEN
        new_cat := CASE NEW.status::text
            WHEN 'done'        THEN 'completed'
            WHEN 'completed'   THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'started'     THEN 'started'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'unstarted'   THEN 'unstarted'
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

    -- UPDATE path: only status changes are lifecycle evidence.
    IF OLD.status_id IS NOT NULL THEN
        SELECT category INTO old_cat FROM team_statuses WHERE id = OLD.status_id;
    END IF;
    IF old_cat IS NULL THEN
        old_cat := CASE OLD.status::text
            WHEN 'done'        THEN 'completed'
            WHEN 'completed'   THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'started'     THEN 'started'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'unstarted'   THEN 'unstarted'
            WHEN 'todo'        THEN 'unstarted'
            ELSE 'backlog'
        END;
    END IF;

    status_changed := OLD.status_id IS DISTINCT FROM NEW.status_id OR OLD.status IS DISTINCT FROM NEW.status;
    IF status_changed THEN
        IF NEW.started_at IS NULL AND new_cat IN ('started', 'completed') THEN
            NEW.started_at = NOW();
        END IF;

        IF NEW.completed_at IS NULL AND new_cat = 'completed' THEN
            NEW.completed_at = NOW();
        ELSIF old_cat = 'completed' AND new_cat <> 'completed' THEN
            NEW.completed_at = NULL;
        END IF;

        IF NEW.cancelled_at IS NULL AND new_cat = 'cancelled' THEN
            NEW.cancelled_at = NOW();
        ELSIF old_cat = 'cancelled' AND new_cat <> 'cancelled' THEN
            NEW.cancelled_at = NULL;
        END IF;
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
            WHEN 'completed'   THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'started'     THEN 'started'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'unstarted'   THEN 'unstarted'
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
            WHEN 'completed'   THEN 'completed'
            WHEN 'cancelled'   THEN 'cancelled'
            WHEN 'started'     THEN 'started'
            WHEN 'in_progress' THEN 'started'
            WHEN 'in_review'   THEN 'started'
            WHEN 'unstarted'   THEN 'unstarted'
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

-- Backfill only timestamps supported by an unambiguous status history entry.
-- created_at/updated_at can represent unrelated activity and are not lifecycle evidence.
WITH history_categories AS (
    SELECT
        ih.issue_id,
        ih.created_at,
        COALESCE(old_status.category,
            CASE LOWER(BTRIM(ih.old_value))
                WHEN 'done' THEN 'completed'
                WHEN 'completed' THEN 'completed'
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'started' THEN 'started'
                WHEN 'in_progress' THEN 'started'
                WHEN 'in_review' THEN 'started'
                WHEN 'unstarted' THEN 'unstarted'
                WHEN 'todo' THEN 'unstarted'
                WHEN 'backlog' THEN 'backlog'
                ELSE NULL
            END
        ) AS old_category,
        COALESCE(new_status.category,
            CASE LOWER(BTRIM(ih.new_value))
                WHEN 'done' THEN 'completed'
                WHEN 'completed' THEN 'completed'
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'started' THEN 'started'
                WHEN 'in_progress' THEN 'started'
                WHEN 'in_review' THEN 'started'
                WHEN 'unstarted' THEN 'unstarted'
                WHEN 'todo' THEN 'unstarted'
                WHEN 'backlog' THEN 'backlog'
                ELSE NULL
            END
        ) AS new_category
    FROM issue_history ih
    JOIN issues history_issue ON history_issue.id = ih.issue_id
    LEFT JOIN LATERAL (
        SELECT MIN(ts.category) AS category
        FROM team_statuses ts
        WHERE ts.team_id = history_issue.team_id
          AND (LOWER(ts.name) = LOWER(BTRIM(ih.old_value)) OR LOWER(ts.slug) = LOWER(BTRIM(ih.old_value)))
        HAVING COUNT(DISTINCT ts.category) = 1
    ) old_status ON TRUE
    LEFT JOIN LATERAL (
        SELECT MIN(ts.category) AS category
        FROM team_statuses ts
        WHERE ts.team_id = history_issue.team_id
          AND (LOWER(ts.name) = LOWER(BTRIM(ih.new_value)) OR LOWER(ts.slug) = LOWER(BTRIM(ih.new_value)))
        HAVING COUNT(DISTINCT ts.category) = 1
    ) new_status ON TRUE
    WHERE ih.field = 'status'
      AND ih.new_value IS NOT NULL
), reliable_transitions AS (
    SELECT *
    FROM history_categories
    WHERE new_category IS NOT NULL
      AND old_category IS DISTINCT FROM new_category
), current_issue_categories AS (
    SELECT
        i.id,
        COALESCE(current_status.category,
            CASE i.status::text
                WHEN 'done' THEN 'completed'
                WHEN 'completed' THEN 'completed'
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'started' THEN 'started'
                WHEN 'in_progress' THEN 'started'
                WHEN 'in_review' THEN 'started'
                WHEN 'unstarted' THEN 'unstarted'
                WHEN 'todo' THEN 'unstarted'
                ELSE 'backlog'
            END
        ) AS current_category
    FROM issues i
    LEFT JOIN team_statuses current_status ON current_status.id = i.status_id
), backfill AS (
    SELECT
        cic.id,
        MIN(rt.created_at) FILTER (WHERE rt.new_category IN ('started', 'completed')) AS started,
        MAX(rt.created_at) FILTER (WHERE rt.new_category = 'completed') AS completed,
        MAX(rt.created_at) FILTER (WHERE rt.new_category = 'cancelled') AS cancelled,
        cic.current_category
    FROM current_issue_categories cic
    LEFT JOIN reliable_transitions rt ON rt.issue_id = cic.id
    GROUP BY cic.id, cic.current_category
)
UPDATE issues i
SET
    started_at = backfill.started,
    completed_at = CASE WHEN backfill.current_category = 'completed' THEN backfill.completed END,
    cancelled_at = CASE WHEN backfill.current_category = 'cancelled' THEN backfill.cancelled END,
    triaged_at = NULL
FROM backfill
WHERE i.id = backfill.id;

-- Add a creation baseline. Migrated completed issues start outside completed;
-- reliable history rows below add completion transitions when their timestamp is known.
WITH current_issue_categories AS (
    SELECT
        i.*,
        COALESCE(ts.category,
            CASE i.status::text
                WHEN 'done' THEN 'completed'
                WHEN 'completed' THEN 'completed'
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'started' THEN 'started'
                WHEN 'in_progress' THEN 'started'
                WHEN 'in_review' THEN 'started'
                WHEN 'unstarted' THEN 'unstarted'
                WHEN 'todo' THEN 'unstarted'
                ELSE 'backlog'
            END
        ) AS current_category
    FROM issues i
    LEFT JOIN team_statuses ts ON ts.id = i.status_id
)
INSERT INTO issue_lifecycle_events (issue_id, from_status_id, to_status_id, event_type, from_category, to_category, created_at, workspace_id, team_id, project_id, cycle_id)
SELECT
    i.id,
    NULL AS from_status_id,
    i.status_id,
    'created',
    NULL AS from_category,
    CASE
        WHEN i.current_category = 'completed' THEN 'backlog'
        ELSE i.current_category
    END,
    i.created_at,
    i.workspace_id,
    i.team_id,
    i.project_id,
    i.cycle_id
FROM current_issue_categories i;

-- Replay only category-changing status history rows whose categories can be resolved reliably.
WITH history_categories AS (
    SELECT
        ih.issue_id,
        ih.created_at,
        COALESCE(old_status.category,
            CASE LOWER(BTRIM(ih.old_value))
                WHEN 'done' THEN 'completed'
                WHEN 'completed' THEN 'completed'
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'started' THEN 'started'
                WHEN 'in_progress' THEN 'started'
                WHEN 'in_review' THEN 'started'
                WHEN 'unstarted' THEN 'unstarted'
                WHEN 'todo' THEN 'unstarted'
                WHEN 'backlog' THEN 'backlog'
                ELSE NULL
            END
        ) AS old_category,
        COALESCE(new_status.category,
            CASE LOWER(BTRIM(ih.new_value))
                WHEN 'done' THEN 'completed'
                WHEN 'completed' THEN 'completed'
                WHEN 'cancelled' THEN 'cancelled'
                WHEN 'started' THEN 'started'
                WHEN 'in_progress' THEN 'started'
                WHEN 'in_review' THEN 'started'
                WHEN 'unstarted' THEN 'unstarted'
                WHEN 'todo' THEN 'unstarted'
                WHEN 'backlog' THEN 'backlog'
                ELSE NULL
            END
        ) AS new_category
    FROM issue_history ih
    JOIN issues history_issue ON history_issue.id = ih.issue_id
    LEFT JOIN LATERAL (
        SELECT MIN(ts.category) AS category
        FROM team_statuses ts
        WHERE ts.team_id = history_issue.team_id
          AND (LOWER(ts.name) = LOWER(BTRIM(ih.old_value)) OR LOWER(ts.slug) = LOWER(BTRIM(ih.old_value)))
        HAVING COUNT(DISTINCT ts.category) = 1
    ) old_status ON TRUE
    LEFT JOIN LATERAL (
        SELECT MIN(ts.category) AS category
        FROM team_statuses ts
        WHERE ts.team_id = history_issue.team_id
          AND (LOWER(ts.name) = LOWER(BTRIM(ih.new_value)) OR LOWER(ts.slug) = LOWER(BTRIM(ih.new_value)))
        HAVING COUNT(DISTINCT ts.category) = 1
    ) new_status ON TRUE
    WHERE ih.field = 'status'
      AND ih.new_value IS NOT NULL
), reliable_transitions AS (
    SELECT *
    FROM history_categories
    WHERE new_category IS NOT NULL
      AND old_category IS DISTINCT FROM new_category
)
INSERT INTO issue_lifecycle_events (issue_id, from_status_id, to_status_id, event_type, from_category, to_category, created_at, workspace_id, team_id, project_id, cycle_id)
SELECT
    rt.issue_id,
    NULL AS from_status_id,
    NULL AS to_status_id,
    'status_changed',
    rt.old_category AS from_category,
    rt.new_category AS to_category,
    rt.created_at,
    i.workspace_id,
    i.team_id,
    i.project_id,
    i.cycle_id
FROM reliable_transitions rt
JOIN issues i ON i.id = rt.issue_id;

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

-- Indexes for analytics queries
CREATE INDEX idx_issues_started_at   ON issues(started_at)   WHERE started_at IS NOT NULL;
CREATE INDEX idx_issues_completed_at ON issues(completed_at) WHERE completed_at IS NOT NULL;
CREATE INDEX idx_issues_cancelled_at ON issues(cancelled_at) WHERE cancelled_at IS NOT NULL;
CREATE INDEX idx_issues_triaged_at   ON issues(triaged_at)   WHERE triaged_at IS NOT NULL;
