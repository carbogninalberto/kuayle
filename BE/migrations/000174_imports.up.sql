-- KUA-100 (deliverable 1): foreign importer jobs. A job is created by a
-- workspace admin, previews a source without writing, then runs in the
-- background. Credentials are encrypted at rest and cleared the moment the job
-- reaches a terminal state; only the per-item report and the external-id map
-- outlive the job so a re-run updates or skips instead of duplicating.
CREATE TABLE import_jobs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq               bigint GENERATED ALWAYS AS IDENTITY,
    workspace_id      uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    created_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    source            text NOT NULL CHECK (source IN ('jira')),
    source_site       text NOT NULL DEFAULT '',
    status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'previewed', 'queued', 'running', 'completed', 'failed', 'cancelled')),
    config            jsonb NOT NULL DEFAULT '{}'::jsonb,
    mapping           jsonb NOT NULL DEFAULT '{}'::jsonb,
    preview           jsonb,
    credentials_enc   text,
    total             integer NOT NULL DEFAULT 0 CHECK (total >= 0),
    processed         integer NOT NULL DEFAULT 0 CHECK (processed >= 0),
    created_count     integer NOT NULL DEFAULT 0 CHECK (created_count >= 0),
    updated_count     integer NOT NULL DEFAULT 0 CHECK (updated_count >= 0),
    skipped_count     integer NOT NULL DEFAULT 0 CHECK (skipped_count >= 0),
    failed_count      integer NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    error             text,
    cancel_requested  boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    started_at        timestamptz,
    finished_at       timestamptz
);
CREATE INDEX import_jobs_queue ON import_jobs (seq) WHERE status = 'queued';
CREATE INDEX import_jobs_workspace ON import_jobs (workspace_id, created_at DESC);

-- The per-item report: one row for every skipped or failed item, read by the
-- UI after the run. A successful item has no row.
CREATE TABLE import_job_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq           bigint GENERATED ALWAYS AS IDENTITY,
    job_id        uuid NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    external_type text NOT NULL,
    external_id   text NOT NULL,
    external_key  text NOT NULL DEFAULT '',
    outcome       text NOT NULL CHECK (outcome IN ('failed', 'skipped')),
    reason        text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX import_job_items_report ON import_job_items (job_id, seq);

-- Stable idempotency map: (source, site, type, external id) -> Kuayle entity.
-- The unique key keeps a re-run from inserting a second copy. content_hash lets
-- an unchanged item be skipped and a changed one be updated.
CREATE TABLE import_external_refs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source        text NOT NULL CHECK (source IN ('jira')),
    source_site   text NOT NULL DEFAULT '',
    external_type text NOT NULL CHECK (external_type IN ('issue', 'comment', 'attachment', 'relation', 'cycle', 'project', 'label')),
    external_id   text NOT NULL,
    external_key  text NOT NULL DEFAULT '',
    external_url  text,
    kuayle_id     uuid NOT NULL,
    content_hash  text,
    last_job_id   uuid REFERENCES import_jobs(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, source, source_site, external_type, external_id)
);
CREATE INDEX import_external_refs_key ON import_external_refs (workspace_id, source, source_site, external_key);
