-- Rollback drops only importer state. Imported issues, comments and assets stay
-- in their normal tables; the external-id map is what is lost, so a re-import
-- after rollback would create new copies rather than update the old ones.
DROP TABLE IF EXISTS import_external_refs;
DROP TABLE IF EXISTS import_job_items;
DROP TABLE IF EXISTS import_jobs;
