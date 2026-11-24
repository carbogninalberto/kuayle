package repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// KUA-257: a push to a base branch (or a pull request merged into it) wakes
// the waiting workflow tasks of that workspace whose published pull request in
// that repository targets the branch, and no other task. A wake already due
// earlier is kept.
func TestBaseBranchMoveWakesOnlyWorkflowTasksWaitingOnThatBase(t *testing.T) {
	db, repo, ctx, graph, claimed := publishingWorkflowTask(t)
	ws := claimed.WorkspaceID
	now := time.Now()
	later := now.Add(time.Hour)
	// The task waits at its CI step until a timeout an hour away.
	for _, step := range []struct{ outcome, node string }{{domain.WorkflowPublished, "review"}, {domain.WorkflowReviewApproved, "ci"}} {
		decision := graph.Decide(claimed.Cursor(), step.outcome, now, nil)
		require.Equal(t, step.node, decision.To.ID)
		entry := WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · " + decision.Reason, WakeAt: &later}
		if step.node == "review" {
			entry = WorkflowEntry{Kind: WorkflowEnterReview, Reason: "Workflow · " + decision.Reason, Review: &domain.AgentReviewRequest{Provider: "codex", Focus: "Be strict."}}
		}
		moved, err := repo.ApplyWorkflow(ctx, *claimed, WorkflowApply{Sequence: claimed.WorkflowSequence, Outcome: step.outcome, Moves: []WorkflowMove{{Decision: decision}}, Entry: entry})
		require.NoError(t, err)
		claimed.WorkflowSequence, claimed.WorkflowNode, claimed.WorkflowVisits, claimed.WorkflowStatus, claimed.State = moved.WorkflowSequence, moved.WorkflowNode, moved.WorkflowVisits, moved.WorkflowStatus, moved.State
	}
	require.Equal(t, "awaiting_review", claimed.State)
	require.Equal(t, "waiting", *claimed.WorkflowStatus)

	installation, linked := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, ws, claimed.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/repo','main')`, linked, installation, ws)
	require.NoError(t, err)
	checkout := func(id uuid.UUID, base string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key)
 VALUES($1,$2,$3,$4,$5,'fixture/repo',$6,'kuayle/wf',$7,'ready',$8)`, id, ws, *claimed.MachineID, claimed.IssueID, linked, base, "/workspace/tasks/"+id.String(), id.String())
		require.NoError(t, err)
	}
	checkout(claimed.CheckoutID, "main")

	// clone copies the waiting task with an attempt of its own. base is its
	// checkout's base branch ("" for no checkout, which is read as main, as
	// the review target does) and pullRequest the repository of the pull
	// request its attempt published ("" for none).
	clone := func(base, pullRequest string) uuid.UUID {
		t.Helper()
		id, run, checkoutID := uuid.New(), uuid.New(), uuid.New()
		_, err := db.Exec(`INSERT INTO agent_tasks SELECT (jsonb_populate_record(NULL::agent_tasks,
 to_jsonb(t)||jsonb_build_object('id',$2::uuid,'request_id',gen_random_uuid(),'run_id',$3::uuid,'checkout_id',$4::uuid))).* FROM agent_tasks t WHERE t.id=$1`, claimed.ID, id, run, checkoutID)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_agent_runs SELECT (jsonb_populate_record(NULL::dev_machine_agent_runs,
 to_jsonb(r)||jsonb_build_object('id',$2::uuid,'task_id',$3::uuid))).* FROM dev_machine_agent_runs r WHERE r.id=$1`, claimed.RunID, run, id)
		require.NoError(t, err)
		if base != "" {
			checkout(checkoutID, base)
		}
		if pullRequest != "" {
			_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,pull_request_number,url,source)
 VALUES($1,$2,$3,'pull_request',$4,'kuayle/wf',8,$5,'published')`, ws, *claimed.MachineID, run, pullRequest, "https://github.com/"+pullRequest+"/pull/8")
			require.NoError(t, err)
		}
		return id
	}
	matching := claimed.ID
	earlier := clone("main", "fixture/repo")
	earlierWake := now.Add(-time.Hour)
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_wake_at=$2 WHERE id=$1`, earlier, earlierWake)
	require.NoError(t, err)
	noCheckout := clone("", "fixture/repo")
	otherBranch := clone("release", "fixture/repo")
	otherRepository := clone("main", "fixture/other")
	noPullRequest := clone("main", "")
	finished := clone("main", "fixture/repo")
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, finished)
	require.NoError(t, err)
	running := clone("main", "fixture/repo")
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_status='running' WHERE id=$1`, running)
	require.NoError(t, err)
	// A task whose workflow was deleted keeps its last status but runs none.
	detached := clone("main", "fixture/repo")
	_, err = db.Exec(`UPDATE agent_tasks SET workflow_version_id=NULL,workflow_wake_at=NULL WHERE id=$1`, detached)
	require.NoError(t, err)

	// wakes is each implementing task's wake time (zero for none).
	wakes := func() map[uuid.UUID]time.Time {
		t.Helper()
		var rows []struct {
			ID   uuid.UUID    `db:"id"`
			Wake sql.NullTime `db:"workflow_wake_at"`
		}
		require.NoError(t, db.Select(&rows, `SELECT id,workflow_wake_at FROM agent_tasks WHERE workspace_id=$1 AND kind='implement'`, ws))
		out := map[uuid.UUID]time.Time{}
		for _, row := range rows {
			out[row.ID] = row.Wake.Time
		}
		return out
	}
	unchanged := func(before, after map[uuid.UUID]time.Time, ids ...uuid.UUID) {
		t.Helper()
		for _, id := range ids {
			require.True(t, before[id].Equal(after[id]), "task %s was woken", id)
		}
	}
	all := []uuid.UUID{matching, earlier, noCheckout, otherBranch, otherRepository, noPullRequest, finished, running, detached}
	before := wakes()

	// Another workspace's push of the same repository and branch wakes nothing.
	n, err := repo.WakeBaseBranchWaits(ctx, uuid.New(), "fixture/repo", "main")
	require.NoError(t, err)
	require.Zero(t, n)
	// Nor does a push of another branch or another repository.
	n, err = repo.WakeBaseBranchWaits(ctx, ws, "fixture/repo", "develop")
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = repo.WakeBaseBranchWaits(ctx, ws, "fixture/unrelated", "main")
	require.NoError(t, err)
	require.Zero(t, n)
	unchanged(before, wakes(), all...)

	n, err = repo.WakeBaseBranchWaits(ctx, ws, "Fixture/Repo", "main")
	require.NoError(t, err)
	require.Equal(t, 3, n)
	after := wakes()
	moment := time.Now()
	for _, id := range []uuid.UUID{matching, noCheckout} {
		require.WithinDuration(t, moment, after[id], 5*time.Second, "woken now")
	}
	require.WithinDuration(t, earlierWake, after[earlier], time.Millisecond, "an earlier wake is kept")
	unchanged(before, after, otherBranch, otherRepository, noPullRequest, finished, running, detached)
	require.True(t, after[otherBranch].After(moment.Add(30*time.Minute)), "still waits for its timeout")

	// The release branch's push wakes the task whose pull request targets it.
	n, err = repo.WakeBaseBranchWaits(ctx, ws, "fixture/repo", "release")
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.WithinDuration(t, time.Now(), wakes()[otherBranch], 5*time.Second)
}
