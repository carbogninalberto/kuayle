package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestIssueAgentWorkAndMergeTargetUseRecordedPublication(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	_, err = repo.MergeTarget(ctx, task.WorkspaceID, &task.IssueID, nil)
	require.ErrorIs(t, err, ErrNoMergeTarget, "nothing to merge before publication")

	require.NoError(t, store.StartPublication(ctx, op))
	commit := "0123456789abcdef0123456789abcdef01234567"
	require.NoError(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: commit, PullRequestNumber: 12, PullRequestURL: "https://github.com/fixture/repo/pull/12"}))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	require.NoError(t, repo.Transition(ctx, *claimed, "awaiting_review", "Published"))
	// A commit only observed in the checkout is never the merge target.
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,commit_sha,source) VALUES($1,$2,$3,'commit','fixture/repo','kuayle/rec-1','ffffffffffffffffffffffffffffffffffffffff','observed')`, task.WorkspaceID, *claimed.MachineID, task.RunID)
	require.NoError(t, err)

	target, err := repo.MergeTarget(ctx, task.WorkspaceID, &task.IssueID, nil)
	require.NoError(t, err)
	require.Equal(t, task.ID, target.TaskID)
	require.Equal(t, "fixture/repo", target.Repository)
	require.Equal(t, 12, target.Number)
	require.Equal(t, commit, target.Commit)
	byTask, err := repo.MergeTarget(ctx, task.WorkspaceID, nil, &task.ID)
	require.NoError(t, err)
	require.Equal(t, target, byTask)

	work, err := repo.IssueAgentWork(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Len(t, work, 1)
	require.Equal(t, "awaiting_review", work[0].State)
	require.Equal(t, 12, *work[0].PullRequestNumber)
	require.Nil(t, work[0].Review)
	_, err = db.Exec(`INSERT INTO agent_task_signals(task_id,run_id,kind,external_id,outcome,name,summary,url,action,request_id) VALUES
 ($1,$2,'review','1','approved','maintainer','','https://github.com/fixture/repo/pull/12','recorded',gen_random_uuid()),
 ($1,$2,'review','2','commented','maintainer','','https://github.com/fixture/repo/pull/12','recorded',gen_random_uuid())`, task.ID, task.RunID)
	require.NoError(t, err)
	work, err = repo.IssueAgentWork(ctx, task.WorkspaceID, task.IssueID)
	require.NoError(t, err)
	require.Equal(t, "approved", *work[0].Review, "comments do not change the decision")

	require.NoError(t, repo.Transition(ctx, *claimed, "completed", "Merged"))
	_, err = repo.MergeTarget(ctx, task.WorkspaceID, nil, &task.ID)
	require.ErrorIs(t, err, ErrNoMergeTarget, "only tasks awaiting review can be merged")
}
