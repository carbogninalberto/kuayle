-- Migration 216 removed graph workflows and detached the tasks that ran them;
-- the schema did not change, and the removed workflows cannot be restored.
-- Rolling back keeps the data as it is: the previous release lists only the
-- remaining pipelines and runs every other task on the Standard flow.
SELECT 1;
