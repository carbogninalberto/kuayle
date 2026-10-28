package repository

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAgentTaskReadPagesRemainPrivateAndBounded(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	for range 26 {
		_, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Independent request", Provider: "codex"})
		require.NoError(t, err)
	}
	first, err := repo.Page(ctx, task.WorkspaceID, *task.ConversationID, nil)
	require.NoError(t, err)
	require.Len(t, first.Tasks, 25)
	require.NotNil(t, first.NextBefore)
	second, err := repo.Page(ctx, task.WorkspaceID, *task.ConversationID, first.NextBefore)
	require.NoError(t, err)
	require.Len(t, second.Tasks, 2)
	require.Nil(t, second.NextBefore)
	seen := map[uuid.UUID]bool{}
	for _, entry := range append(first.Tasks, second.Tasks...) {
		require.False(t, seen[entry.ID])
		seen[entry.ID] = true
	}
	_, err = db.Exec(`INSERT INTO agent_task_transitions(task_id,from_state,to_state,reason) SELECT $1,'running','waiting_input','Input required' FROM generate_series(1,104)`, task.ID)
	require.NoError(t, err)
	history, err := repo.History(ctx, task.WorkspaceID, task.ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Transitions, 100)
	require.NotNil(t, history.NextAfter)
	moreHistory, err := repo.History(ctx, task.WorkspaceID, task.ID, *history.NextAfter)
	require.NoError(t, err)
	require.Len(t, moreHistory.Transitions, 6)
	require.Nil(t, moreHistory.NextAfter)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,created_at,encrypted_output_secrets) SELECT gen_random_uuid(),workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,'succeeded',now()-make_interval(secs=>n),'ciphertext-never-expose' FROM dev_machine_agent_runs CROSS JOIN generate_series(1,26) n WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	runs, err := repo.Runs(ctx, task.WorkspaceID, task.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs.Runs, 25)
	require.NotNil(t, runs.NextBefore)
	olderRuns, err := repo.Runs(ctx, task.WorkspaceID, task.ID, runs.NextBefore)
	require.NoError(t, err)
	require.Len(t, olderRuns.Runs, 2)
	require.Nil(t, olderRuns.NextBefore)
	encoded, err := json.Marshal(runs)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "ciphertext-never-expose")
	require.NotContains(t, string(encoded), "encrypted_output_secrets")
	_, err = db.Exec(`INSERT INTO dev_machine_log_chunks(workspace_id,machine_id,agent_run_id,stream,sequence,content) SELECT workspace_id,machine_id,id,'stdout',n,'Redacted progress' FROM dev_machine_agent_runs CROSS JOIN generate_series(1,105) n WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	logs, err := repo.Logs(ctx, task.WorkspaceID, task.ID, task.RunID, 0)
	require.NoError(t, err)
	require.Len(t, logs.Logs, 100)
	require.NotNil(t, logs.NextAfter)
	moreLogs, err := repo.Logs(ctx, task.WorkspaceID, task.ID, task.RunID, *logs.NextAfter)
	require.NoError(t, err)
	require.Len(t, moreLogs.Logs, 5)
	require.Nil(t, moreLogs.NextAfter)
	require.Greater(t, moreLogs.Logs[0].ID, logs.Logs[99].ID)
	foreign := first.Tasks[0]
	_, err = repo.Logs(ctx, task.WorkspaceID, foreign.ID, task.RunID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Runs(ctx, task.WorkspaceID, foreign.ID, &task.RunID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	otherChat, err := NewConversationRepository(db).Create(ctx, task.WorkspaceID, "Different conversation")
	require.NoError(t, err)
	_, err = repo.Page(ctx, task.WorkspaceID, otherChat.ID, &task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	// Current workspace role and original issue scope apply to every replay page.
	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = repo.Page(ctx, task.WorkspaceID, *task.ConversationID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.History(ctx, task.WorkspaceID, task.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Runs(ctx, task.WorkspaceID, task.ID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Logs(ctx, task.WorkspaceID, task.ID, task.RunID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	require.NotNil(t, claimed.LeaseToken)
}

func TestAgentTaskAttemptsCannotBypassConversationReadsThroughManualMachineAPI(t *testing.T) {
	db, tasks, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, tasks, ctx, task, fixture)
	machine := *claimed.MachineID
	manualID := uuid.New()
	_, err := db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status) VALUES($1,$2,$3,'codex','autonomous','Manual work','["codex"]',60,'succeeded')`, manualID, task.WorkspaceID, machine)
	require.NoError(t, err)
	for _, run := range []uuid.UUID{task.RunID, manualID} {
		_, err = db.Exec(`INSERT INTO dev_machine_events(workspace_id,machine_id,agent_run_id,source,event_type,payload) VALUES($1,$2,$3,'agent','result','{"text":"execution detail"}')`, task.WorkspaceID, machine, run)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO dev_machine_log_chunks(workspace_id,machine_id,agent_run_id,stream,sequence,content) VALUES($1,$2,$3,'stdout',1,'execution output')`, task.WorkspaceID, machine, run)
		require.NoError(t, err)
	}
	machines := fixture.repository
	events, err := machines.ListEvents(ctx, task.WorkspaceID, machine, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, manualID, *events[0].AgentRunID)
	logs, err := machines.ListLogs(ctx, task.WorkspaceID, machine, nil, 0, 100)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, manualID, *logs[0].AgentRunID)
	logs, err = machines.ListLogs(ctx, task.WorkspaceID, machine, &task.RunID, 0, 100)
	require.NoError(t, err)
	require.Empty(t, logs)
	allowed, err := tasks.Logs(ctx, task.WorkspaceID, task.ID, task.RunID, 0)
	require.NoError(t, err)
	require.Len(t, allowed.Logs, 1)
	_, err = db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = tasks.Logs(ctx, task.WorkspaceID, task.ID, task.RunID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestTaskCheckoutsCannotBecomeManualRuns(t *testing.T) {
	db, tasks, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, tasks, ctx, task, fixture)
	machine := *claimed.MachineID
	installation, repositoryID, manualCheckout := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/repo','main')`, repositoryID, installation, task.WorkspaceID)
	require.NoError(t, err)
	for _, entry := range []struct {
		id    uuid.UUID
		scope string
	}{{task.CheckoutID, task.ID.String()}, {manualCheckout, ""}} {
		_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main',$6,$7,'ready',$8)`, entry.id, task.WorkspaceID, machine, task.IssueID, repositoryID, "kuayle/"+entry.id.String(), "/workspace/tasks/"+entry.id.String(), entry.scope)
		require.NoError(t, err)
	}
	machines := fixture.repository
	got, err := machines.GetCheckout(ctx, task.WorkspaceID, machine, task.CheckoutID)
	require.NoError(t, err)
	require.Nil(t, got, "manual endpoints must not disclose task checkout metadata")
	listed, err := machines.ListCheckouts(ctx, task.WorkspaceID, machine)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, manualCheckout, listed[0].ID)
	dispatch := domain.WithTaskExecution(ctx, *claimed)
	got, err = machines.GetCheckout(dispatch, task.WorkspaceID, machine, task.CheckoutID)
	require.NoError(t, err)
	require.NotNil(t, got)
	listed, err = machines.ListCheckouts(dispatch, task.WorkspaceID, machine)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, task.CheckoutID, listed[0].ID)
	// The write boundary must reject a known task checkout ID even if a caller
	// bypassed the service lookup; native work cannot lose its task association.
	run := &domain.DevMachineAgentRun{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine, CheckoutID: &task.CheckoutID}
	require.ErrorIs(t, machines.CreateAgentRun(ctx, run, &domain.DevMachineOperation{Generation: 1}), ErrAgentTaskUnauthorized)
	_, err = db.Exec(`DELETE FROM conversations WHERE id=$1`, task.ConversationID)
	require.NoError(t, err)
	got, err = machines.GetCheckout(ctx, task.WorkspaceID, machine, task.CheckoutID)
	require.NoError(t, err)
	require.Nil(t, got)
	require.ErrorIs(t, machines.CreateAgentRun(ctx, run, &domain.DevMachineOperation{Generation: 1}), ErrAgentTaskUnauthorized)
}

// A task checkout adds a worktree for its repository to a machine that served
// another repository, without reassigning the machine's own repository. Manual
// checkouts of another repository are still refused.
func TestTaskCheckoutsShareAMachineAcrossRepositories(t *testing.T) {
	db, tasks, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, tasks, ctx, task, fixture)
	machine := *claimed.MachineID
	installation, first, second := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/first','main'),($4,$2,$3,2,'fixture/second','main')`, first, installation, task.WorkspaceID, second)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET repository_affinity_id=$2,repo_url='https://github.com/fixture/first',repo_owner='fixture',repo_name='first' WHERE id=$1`, machine, first)
	require.NoError(t, err)

	dispatch := domain.WithTaskExecution(ctx, *claimed)
	checkout := &domain.DevMachineCheckout{ID: task.CheckoutID, WorkspaceID: task.WorkspaceID, MachineID: machine, IssueID: task.IssueID,
		GitHubRepoID: second, RepositoryFullName: "fixture/second", BaseBranch: "main", WorkingBranch: "kuayle/rec-1-" + task.ID.String(),
		WorkspacePath: "/workspace/tasks/rec-1-" + task.ID.String(), Status: "queued", ScopeKey: task.ID.String()}
	operation := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine, Action: domain.DevMachineOpCheckoutIssue,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "checkout-issue:" + task.IssueID.String() + ":" + task.ID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 5}
	require.NoError(t, fixture.repository.CreateCheckout(dispatch, checkout, operation))
	var affinity uuid.UUID
	var repoURL string
	require.NoError(t, db.QueryRow(`SELECT repository_affinity_id,repo_url FROM dev_machines WHERE id=$1`, machine).Scan(&affinity, &repoURL))
	require.Equal(t, first, affinity)
	require.Equal(t, "https://github.com/fixture/first", repoURL, "the root checkout keeps its repository")

	manual := &domain.DevMachineCheckout{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine, IssueID: task.IssueID,
		GitHubRepoID: second, RepositoryFullName: "fixture/second", BaseBranch: "main", WorkingBranch: "kuayle/rec-1",
		WorkspacePath: "/workspace/tasks/rec-1", Status: "queued"}
	manualOperation := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: machine, Action: domain.DevMachineOpCheckoutIssue,
		Status: domain.DevMachineOpStatusPending, Generation: 1, IdempotencyKey: "checkout-issue:" + task.IssueID.String() + ":", RequestedByUserID: &task.CreatorID, MaxAttempts: 5}
	require.ErrorIs(t, fixture.repository.CreateCheckout(ctx, manual, manualOperation), ErrCheckoutMachineConflict)
}
