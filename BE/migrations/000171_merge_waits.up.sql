-- An approved merge that Kuayle could not finish at once because it brought
-- the branch up to date with its base (GitHub's update-branch) waits for the
-- checks on the new head and then merges (KUA-207, KUA-209).
--
-- An assistant merge approval waits as `waiting_checks`: still unresolved, its
-- payload holds the wait (the approved commit, the head Kuayle merges, the
-- update rounds and the deadline), and next_check_at is when the poller reads
-- the pull request again. The poller claims due rows with SKIP LOCKED and
-- pushes next_check_at forward as its lease.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_status_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_status_check
    CHECK (status IN ('pending','approving','waiting_checks','approved','denied','expired','failed'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_check
    CHECK ((status IN ('pending','approving','waiting_checks')) = (resolved_at IS NULL));
ALTER TABLE conversation_approvals ADD COLUMN next_check_at TIMESTAMPTZ;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_waiting_check
    CHECK ((status = 'waiting_checks') = (next_check_at IS NOT NULL));
CREATE INDEX conversation_approvals_waiting ON conversation_approvals (next_check_at) WHERE status = 'waiting_checks';

-- A workflow task merges its pull request when a person approves its ship
-- approval (or the merge step right after it). The step being merged keeps the
-- same wait, with the approver, while the task waits on it.
ALTER TABLE agent_task_workflow_steps ADD COLUMN merge JSONB CHECK (merge IS NULL OR octet_length(merge::text) <= 8192);
