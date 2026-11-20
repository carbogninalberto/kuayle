ALTER TABLE issue_templates
    ADD COLUMN recurrence_lease_until TIMESTAMPTZ,
    ADD COLUMN last_run_at TIMESTAMPTZ,
    ADD COLUMN last_error TEXT;

CREATE TABLE issue_template_occurrences (
    template_id UUID NOT NULL REFERENCES issue_templates(id) ON DELETE CASCADE,
    scheduled_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (template_id, scheduled_at)
);
