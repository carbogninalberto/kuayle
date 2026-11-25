ALTER TABLE dev_machine_workspace_policies DROP COLUMN task_files_max;

-- Scan results are transient (a workspace keeps an hour of them); tree results
-- over the former bound are removed so it can be restored.
DELETE FROM dev_machine_worktree_scans WHERE result IS NOT NULL AND octet_length(result::text) > 65536;
ALTER TABLE dev_machine_worktree_scans DROP CONSTRAINT dev_machine_worktree_scans_result_check;
ALTER TABLE dev_machine_worktree_scans ADD CONSTRAINT dev_machine_worktree_scans_result_check
    CHECK (result IS NULL OR octet_length(result::text) <= 65536);
