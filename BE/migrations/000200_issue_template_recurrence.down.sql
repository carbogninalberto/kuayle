DROP TABLE issue_template_occurrences;

ALTER TABLE issue_templates
    DROP COLUMN last_error,
    DROP COLUMN last_run_at,
    DROP COLUMN recurrence_lease_until;
