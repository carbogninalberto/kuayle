-- KUA-280: a task's files at a commit. A task's file listing read on its
-- machine (a "tree" worktree scan) holds up to 30,000 paths, more than the
-- 64 KiB every scan result had. A tree result may take 4 MiB: 30,000 paths
-- averaging about 135 bytes in jsonb's text form (about 4 bytes per path more
-- than the path; this repository's paths average 42 bytes). Every other kind
-- keeps 64 KiB.
ALTER TABLE dev_machine_worktree_scans DROP CONSTRAINT dev_machine_worktree_scans_result_check;
ALTER TABLE dev_machine_worktree_scans ADD CONSTRAINT dev_machine_worktree_scans_result_check
    CHECK (result IS NULL OR octet_length(result::text) <= CASE WHEN request->>'kind' = 'tree' THEN 4194304 ELSE 65536 END);

-- How many files a task's file listing holds, set by the workspace's admins:
-- from 100 to 30,000, the most a GitHub checkout materialises and a tree
-- result's bound is sized for.
ALTER TABLE dev_machine_workspace_policies
    ADD COLUMN task_files_max int NOT NULL DEFAULT 30000 CHECK (task_files_max BETWEEN 100 AND 30000);
