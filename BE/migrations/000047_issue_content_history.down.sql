DROP TRIGGER IF EXISTS issue_content_history ON issues;
DROP FUNCTION IF EXISTS kuayle_record_issue_content_history();
-- Keep nullable actors on rollback to preserve unattributed historical records.
