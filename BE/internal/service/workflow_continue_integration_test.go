package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestWorkflowContinuationKeepsTheCheckoutAndPublishesToTheSamePullRequest(t *testing.T) {
	f := newProfileFixture(t)
	sourceProfile := f.storedProfile(t, "Original agent", "claude-code", "", "")
	targetProfile := f.storedProfile(t, "Repair agent", "codex", "gpt-5.5", "medium")
	reviewer, err := f.repo.CreateProfile(f.ctx, f.ws, f.user, domain.AgentProfileInput{Name: "Other agent", Behaviour: domain.AgentBehaviourReviews, Harness: "codex"})
	require.NoError(t, err)
	graph := domain.ReviewLoopWorkflowGraph(targetProfile.ID, reviewer.ID)
	graph.Nodes[1].Label, graph.Nodes[1].Prompt = "Repair", "Run the checks again and repair the release."
	graph.Nodes = append(graph.Nodes, domain.WorkflowNode{ID: "prepare", Label: "Prepare the repair", Type: domain.WorkflowNodeAgent, ProfileID: &targetProfile.ID})
	graph.Edges[0].Target = "prepare"
	graph.Edges = append(graph.Edges, domain.WorkflowEdge{ID: "prepared", Source: "prepare", Target: "implement", Condition: domain.WorkflowAgentFinished})
	graph.Budget = domain.WorkflowBudget{MaxIterations: 3, MaxMinutes: 45}
	targetGraph, err := json.Marshal(graph)
	require.NoError(t, err)
	source, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Original workflow", "", oneStepGraph(t, sourceProfile.ID), "created")
	require.NoError(t, err)
	target, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Release repair", "", targetGraph, "created")
	require.NoError(t, err)
	sourceID := source.ID.String()
	version, err := f.repo.ResolveWorkflow(f.ctx, f.ws, f.issue, &sourceID)
	require.NoError(t, err)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	repo := f.tasks.Repo
	task, err := repo.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Fix the release", Provider: "claude-code", Workflow: version})
	require.NoError(t, err)
	get := func() *domain.AgentTask {
		current, err := repo.Get(f.ctx, f.ws, task.ID)
		require.NoError(t, err)
		return current
	}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := f.db.Exec(query, args...)
		require.NoError(t, err)
	}
	claimEntry := func() *domain.AgentTask {
		t.Helper()
		exec(`UPDATE agent_tasks SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '5 minutes'
 WHERE id=$1 AND workflow_status='entering' AND workflow_wake_at<=now()`, task.ID)
		claimed := get()
		require.NotNil(t, claimed.LeaseToken)
		return claimed
	}
	machine, installation, repoID := uuid.New(), uuid.New(), uuid.New()
	const branch, path, pullURL = "kuayle/eng-1", "/workspace/tasks/eng-1", "https://github.com/acme/app/pull/12"
	const nextCommit = "1123456789abcdef0123456789abcdef01234567"
	exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,machine_size,cpu_millis,memory_mb,disk_gb,status,desired_status)
 VALUES($1,$2,$3,$4,'Test','small',1000,1024,20,'running','running')`, machine, f.ws, f.user, strings.ReplaceAll(machine.String(), "-", "")[:16])
	exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,12,'acme','Organization',$3)`, installation, f.ws, f.user)
	exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,12,'acme/app','main')`, repoID, installation, f.ws)
	exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key)
 VALUES($1,$2,$3,$4,$5,'acme/app','main',$6,$7,'ready',$8)`, task.CheckoutID, f.ws, machine, f.issue, repoID, branch, path, task.ID.String())
	exec(`UPDATE agent_tasks SET machine_id=$2,state='publishing',lease_token=gen_random_uuid(),lease_expires_at=now()+interval '5 minutes' WHERE id=$1`, task.ID, machine)
	exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,checkout_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at,native_session_id)
 VALUES($1,$2,$3,$4,$5,$6,'claude-code','autonomous','Original','["claude"]',60,'succeeded',now(),$7)`, task.RunID, f.ws, machine, task.ID, f.user, task.CheckoutID, uuid.NewString())
	store := repository.NewDevMachineRepository(f.db)
	require.NoError(t, store.UpsertPolicy(f.ctx, f.machines.policy))
	publish := func(t *testing.T, claimed domain.AgentTask, previous, commit string) {
		t.Helper()
		_, err := repo.EnsurePublication(f.ctx, claimed)
		require.NoError(t, err)
		var op domain.DevMachineOperation
		require.NoError(t, f.db.Get(&op, `UPDATE dev_machine_operations SET status='leased',lease_owner='workflow-switch-test',
 lease_expires_at=now()+interval '5 minutes',attempts=attempts+1 WHERE agent_run_id=$1 AND action='publish_agent' AND status='pending'
 RETURNING *`, claimed.RunID))
		require.Equal(t, domain.DevMachineOpPublishAgent, op.Action)
		var checkout struct {
			ID     uuid.UUID `db:"id"`
			Branch string    `db:"working_branch"`
			Path   string    `db:"workspace_path"`
		}
		require.NoError(t, f.db.Get(&checkout, `SELECT c.id,c.working_branch,c.workspace_path FROM dev_machine_checkouts c
 JOIN dev_machine_agent_runs r ON c.id=r.checkout_id WHERE r.id=$1`, op.AgentRunID))
		require.Equal(t, task.CheckoutID, checkout.ID)
		require.Equal(t, branch, checkout.Branch)
		require.Equal(t, path, checkout.Path)
		publication, err := store.GetPublication(f.ctx, op)
		require.NoError(t, err)
		require.Equal(t, previous, publication.PublishedCommit, "publication sees what was pushed on the preserved branch")
		require.NoError(t, store.StartPublication(f.ctx, op))
		require.NoError(t, store.CompletePublication(f.ctx, op, domain.AgentPublicationResult{Branch: branch, Commit: commit, PullRequestNumber: 12, PullRequestURL: pullURL}))
		require.NoError(t, store.CompleteOperation(f.ctx, op.ID, *op.LeaseOwner))
	}
	publish(t, *get(), "", reviewedCommit)
	input := domain.WorkflowSwitchTarget{WorkflowID: target.ID.String(), Node: "implement"}
	refuse := func(input domain.WorkflowSwitchTarget, reason string) {
		t.Helper()
		for _, apply := range []bool{false, true} {
			plan, moved, err := f.tasks.SwitchWorkflow(f.ctx, f.ws, task.ID, input, apply)
			require.NoError(t, err)
			require.Nil(t, moved)
			require.Equal(t, reason, plan.Refused)
		}
	}
	refuse(input, domain.WorkflowSwitchNotAtRest)
	exec(`UPDATE agent_tasks SET state='blocked',workflow_paused_at=now()-interval '10 minutes',
 workflow_started_at=now()-interval '40 minutes',workflow_iterations=9,workflow_visits='{"implement":4}',
 workflow_budget_extensions=2 WHERE id=$1`, task.ID)
	before := get()
	refuse(domain.WorkflowSwitchTarget{WorkflowID: target.ID.String(), Node: "missing"}, domain.WorkflowSwitchStepMissing)
	refuse(domain.WorkflowSwitchTarget{WorkflowID: target.ID.String(), Node: "start"}, domain.WorkflowSwitchStepInvalid)
	refuse(domain.WorkflowSwitchTarget{WorkflowID: uuid.NewString(), Node: "implement"}, domain.WorkflowSwitchUnavailable)
	stale := uuid.New()
	refuse(domain.WorkflowSwitchTarget{WorkflowID: target.ID.String(), Node: "implement", ExpectedVersionID: &stale}, domain.WorkflowSwitchTargetStale)
	archived, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Archived workflow", "", targetGraph, "created")
	require.NoError(t, err)
	require.NoError(t, f.repo.ArchiveWorkflow(f.ctx, f.ws, archived.ID, f.user))
	refuse(domain.WorkflowSwitchTarget{WorkflowID: archived.ID.String(), Node: "implement"}, domain.WorkflowSwitchArchived)
	allowed := f.machines.policy.AllowedProviders
	f.machines.policy.AllowedProviders = json.RawMessage(`["claude-code"]`)
	refuse(input, domain.WorkflowSwitchUnusable)
	f.machines.policy.AllowedProviders = allowed
	plan, moved, err := f.tasks.SwitchWorkflow(f.ctx, f.ws, task.ID, input, false)
	require.NoError(t, err)
	require.Nil(t, moved)
	require.Empty(t, plan.Refused)
	require.Equal(t, []string{"task", "machine", "worktree", "checkout", "branch", "pull_request"}, plan.Continuation.Carries)
	require.Equal(t, graph.Budget, plan.Continuation.Budget)
	require.Equal(t, []string{"visits", "iterations", "budget_extensions", "time_budget"}, plan.Continuation.Resets)
	require.EqualValues(t, 1800, plan.Continuation.TimeSpentSeconds)
	require.Equal(t, before.WorkflowVersionID, get().WorkflowVersionID, "preview is read-only")
	input.ExpectedVersionID = &plan.Continuation.ToVersionID
	_, moved, err = f.tasks.SwitchWorkflow(f.ctx, f.ws, task.ID, input, true)
	require.NoError(t, err)
	require.NotNil(t, moved)
	require.Nil(t, moved.LeaseToken)
	require.Nil(t, moved.WorkflowPausedAt)
	require.Zero(t, moved.WorkflowIterations)
	require.Zero(t, moved.WorkflowExtensions)
	require.Equal(t, map[string]int{"implement": 1}, moved.Cursor().Visits)
	completed, err := repo.CompleteMergedPullRequest(f.ctx, f.ws, "acme/app", 12)
	require.NoError(t, err)
	require.Zero(t, completed, "an old merge event cannot complete a task entering a new agent step")
	_, _, err = repo.Accept(f.ctx, f.ws, task.ID, before.Generation, false)
	require.ErrorIs(t, err, repository.ErrAgentTaskConflict)
	_, err = repo.DecideWorkflow(f.ctx, f.ws, task.ID, repository.WorkflowApply{
		Version: before.WorkflowVersionID, Sequence: moved.WorkflowSequence, Hold: true,
		Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterFail, Reason: "Old decision"},
	})
	require.ErrorIs(t, err, repository.ErrAgentTaskConflict)
	require.NoError(t, f.tasks.Workflow().Evaluate(f.ctx, *claimEntry()))
	queued := get()
	require.Equal(t, "queued", queued.State)
	require.Equal(t, task.ID, queued.ID)
	require.Equal(t, &machine, queued.MachineID)
	require.Equal(t, task.CheckoutID, queued.CheckoutID)
	require.Equal(t, task.Generation+1, queued.Generation)
	require.Equal(t, 1, queued.WorkflowIterations)
	require.Equal(t, "codex", queued.Provider)
	require.Equal(t, "gpt-5.5", queued.Model)
	require.Equal(t, "medium", queued.Thinking)
	require.Contains(t, queued.Prompt, "Repair (visit 1)")
	require.Contains(t, queued.Prompt, graph.Nodes[1].Prompt)
	previous, err := repo.PreviousAttempt(f.ctx, *queued)
	require.NoError(t, err)
	require.NotEmpty(t, previous.NativeSessionID)
	require.Empty(t, taskSession(*queued, previous).ResumeID)
	retained, err := repo.WorkflowPullRequest(f.ctx, *queued)
	require.NoError(t, err)
	require.Equal(t, pullURL, retained.PullRequestURL)

	exec(`UPDATE agent_tasks SET state='running',lease_token=gen_random_uuid(),lease_expires_at=now()+interval '5 minutes' WHERE id=$1`, task.ID)
	exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,checkout_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,'codex','autonomous','Repair','["codex"]',60,'succeeded',now())`, queued.RunID, f.ws, machine, task.ID, f.user, task.CheckoutID)
	require.NoError(t, f.tasks.Workflow().AgentFinished(f.ctx, *get()))
	publish(t, *get(), reviewedCommit, nextCommit)
	publication, err := repo.Publication(f.ctx, f.ws, task.ID, queued.RunID)
	require.NoError(t, err)
	require.Equal(t, pullURL, *publication.PullRequestURL)
	events, err := repo.WorkflowPauses(f.ctx, f.ws, task.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, domain.WorkflowContinueAction, events[0].Action)
	require.Equal(t, &source.ID, events[0].FromWorkflowID)
	require.Equal(t, &target.ID, events[0].ToWorkflowID)
	require.Equal(t, "implement", events[0].FromNode)
	require.Equal(t, "Repair", events[0].ToNodeLabel)
	require.Equal(t, moved.WorkflowSequence, events[0].Sequence)

	// A switch from a wait enters a new native step even if the checkout
	// already has a publication or there was an earlier decision.
	nativeGraph, err := json.Marshal(domain.FullCycleWorkflowGraph(targetProfile.ID, reviewer.ID))
	require.NoError(t, err)
	native, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Native delivery", "", nativeGraph, "created")
	require.NoError(t, err)
	exec(`UPDATE agent_tasks SET state='awaiting_review',workflow_status='waiting',run_id=gen_random_uuid() WHERE id=$1`, task.ID)
	for _, node := range []string{"ci", "ship", "merge"} {
		current := get()
		_, moved, err := f.tasks.SwitchWorkflow(f.ctx, f.ws, task.ID, domain.WorkflowSwitchTarget{WorkflowID: native.ID.String(), Node: node}, true)
		require.NoError(t, err)
		require.NotNil(t, moved)
		require.NoError(t, f.tasks.Workflow().Evaluate(f.ctx, *claimEntry()))
		waiting := get()
		require.Equal(t, domain.WorkflowStatusWaiting, *waiting.WorkflowStatus)
		require.Equal(t, current.RunID, waiting.RunID, "entering a native step keeps the prior attempt")
		require.Equal(t, current.Generation, waiting.Generation)
		step, err := repo.CurrentWorkflowStep(f.ctx, *waiting)
		require.NoError(t, err)
		require.Equal(t, node, step.NodeID)
		require.Equal(t, 1, step.Visit)
		require.Nil(t, step.FinishedAt)
		require.Nil(t, step.MergeWait())
		require.Nil(t, step.CIRecord())
	}
	mergeTarget, err := repo.MergeTarget(f.ctx, f.ws, nil, &task.ID)
	require.NoError(t, err)
	require.Equal(t, nextCommit, mergeTarget.Commit)
	require.Equal(t, pullURL, mergeTarget.URL)
	completed, err = repo.CompleteMergedPullRequest(f.ctx, f.ws, "acme/app", 12)
	require.NoError(t, err)
	require.Equal(t, 1, completed)
	require.Equal(t, "completed", get().State)
}
