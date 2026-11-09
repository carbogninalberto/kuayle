-- AI overviews summarise a wiki registry over a period. They belong to one
-- team or project wiki and are read through the repository's wiki visibility
-- predicates, like its pages. A schedule on the wiki's settings can generate
-- them; overview_next_run_at is the claim that makes each slot run once
-- across replicas.
CREATE TABLE wiki_registry_overviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    team_id UUID REFERENCES teams(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    content TEXT NOT NULL CHECK (octet_length(content) <= 131072),
    trigger VARCHAR(16) NOT NULL CHECK (trigger IN ('manual', 'schedule')),
    generated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT wiki_registry_overviews_single_scope CHECK ((team_id IS NULL) <> (project_id IS NULL)),
    CONSTRAINT wiki_registry_overviews_period CHECK (period_end >= period_start AND period_end - period_start < 31)
);

CREATE INDEX wiki_registry_overviews_team_idx ON wiki_registry_overviews (team_id, generated_at DESC) WHERE team_id IS NOT NULL;
CREATE INDEX wiki_registry_overviews_project_idx ON wiki_registry_overviews (project_id, generated_at DESC) WHERE project_id IS NOT NULL;

ALTER TABLE wiki_settings
    ADD COLUMN overview_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN overview_frequency VARCHAR(8) NOT NULL DEFAULT 'weekly' CHECK (overview_frequency IN ('daily', 'weekly')),
    ADD COLUMN overview_weekday SMALLINT NOT NULL DEFAULT 1 CHECK (overview_weekday BETWEEN 0 AND 6),
    ADD COLUMN overview_time VARCHAR(5) NOT NULL DEFAULT '09:00' CHECK (overview_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    ADD COLUMN overview_period_days SMALLINT NOT NULL DEFAULT 7 CHECK (overview_period_days BETWEEN 1 AND 31),
    -- The person whose visibility scheduled runs use: who last saved the schedule.
    ADD COLUMN overview_actor UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN overview_next_run_at TIMESTAMPTZ,
    ADD COLUMN overview_last_run_at TIMESTAMPTZ,
    ADD COLUMN overview_last_error TEXT CHECK (char_length(overview_last_error) <= 500);

CREATE INDEX wiki_settings_overview_due_idx ON wiki_settings (overview_next_run_at) WHERE overview_enabled AND overview_next_run_at IS NOT NULL;
