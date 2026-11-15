package repository

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// pauseFixture is a task on the review-loop workflow whose implementing
// agent is running, claimed by the worker.
type pauseFixture struct {
	t       *testing.T
	db      *sqlx.DB
	repo    *AgentTaskRepository
	ctx     context.Context
	ws      uuid.UUID
	task    domain.AgentTask
	claimed *domain.AgentTask
	graph   domain.WorkflowGraph
}

func newPauseFixture(t *testing.T) *pauseFixture {
	t.Helper()
	db, repo, ctx, legacy, fixture := taskRecoveryFixture(t)
	_, err := db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)
	ws := legacy.WorkspaceID
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	graph := domain.ReviewLoopWorkflowGraph(implementer, reviewer)
	graph.Budget = domain.WorkflowBudget{MaxMinutes: 60}
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, fixture.userID, "Review loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(ctx, ws, legacy.IssueID, &chosen)
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, *legacy.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code", Workflow: version})
	require.NoError(t, err)
	claimed := prepareTaskStart(t, db, repo, ctx, *task, fixture)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	// The worker's lease outlives a slow test machine.
	_, err = db.Exec(`UPDATE agent_tasks SET lease_expires_at=now()+interval '10 minutes' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	return &pauseFixture{t: t, db: db, repo: repo, ctx: ctx, ws: ws, task: *task, claimed: claimed, graph: graph}
}

func (f *pauseFixture) current() domain.AgentTask {
	f.t.Helper()
	var task domain.AgentTask
	require.NoError(f.t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, f.task.ID))
	return task
}

func (f *pauseFixture) exec(query string, args ...any) {
	f.t.Helper()
	_, err := f.db.Exec(query, args...)
	require.NoError(f.t, err)
}

// claim is the worker's next claim of the fixture's task, or nil.
func (f *pauseFixture) claim() *domain.AgentTask {
	f.t.Helper()
	f.exec(`UPDATE agent_tasks SET next_poll_at=now(),lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, f.task.ID)
	claimed, err := f.repo.Claim(f.ctx)
	require.NoError(f.t, err)
	if claimed != nil {
		require.Equal(f.t, f.task.ID, claimed.ID)
	}
	return claimed
}

// toPublish is the move the worker applies when the agent finished.
func (f *pauseFixture) toPublish(task domain.AgentTask) WorkflowApply {
	decision := f.graph.Decide(task.Cursor(), domain.WorkflowAgentFinished, time.Now(), nil)
	return WorkflowApply{Sequence: task.WorkflowSequence, Outcome: domain.WorkflowAgentFinished, Moves: []WorkflowMove{{Decision: decision}}, Entry: WorkflowEntry{Kind: WorkflowEnterPublish, Reason: "Workflow · " + decision.Reason}}
}

// cleanup is the worker cleaning up an attempt the task no longer runs.
func (f *pauseFixture) cleanup() {
	f.t.Helper()
	claimed := f.claim()
	require.NotNil(f.t, claimed, "cleanup goes on while the workflow is paused")
	require.Equal(f.t, "cancelling", claimed.State)
	require.NoError(f.t, f.repo.CancelAttempt(f.ctx, *claimed))
	f.exec(`UPDATE dev_machine_operations SET status='completed' WHERE agent_run_id=$1 AND action='cancel_agent'`, claimed.RunID)
	require.NoError(f.t, f.repo.FinishCancellation(f.ctx, *claimed, "Runtime cancellation confirmed"))
}

func (f *pauseFixture) step(sequence int) domain.WorkflowStep {
	f.t.Helper()
	var step domain.WorkflowStep
	require.NoError(f.t, f.db.Get(&step, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 AND sequence=$2`, f.task.ID, sequence))
	return step
}

// "Pause now" while the agent works stops the attempt, keeps the task's
// position and resumes it on the same step as a new attempt that continues
// the native session, leaving the paused time out of the budget.
func TestWorkflowPauseNowStopsTheAgentAndResumeContinuesTheStep(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()

	_, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a workflow that is not paused does not resume")
	_, err = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, "later")
	require.ErrorIs(t, err, ErrConversationInvalid)

	paused, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
	require.NoError(t, err)
	require.Equal(t, "cancelling", paused.State, "the attempt is stopped through the usual cleanup")
	require.NotNil(t, paused.WorkflowPausedAt)
	require.False(t, paused.FailurePending, "nothing failed")
	require.Nil(t, paused.LeaseToken)

	// The step the worker was finishing when the pause arrived is refused.
	_, err = f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(before))
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	_, err = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "a paused workflow is not paused again")
	_, err = f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the stopped attempt is still being cleaned up")

	f.cleanup()
	blocked := f.current()
	require.Equal(t, "blocked", blocked.State)
	require.Equal(t, "Paused", blocked.Reason)
	require.Equal(t, domain.WorkflowStatusRunning, *blocked.WorkflowStatus)
	require.Equal(t, before.MachineID, blocked.MachineID, "the machine and the checkout stay the task's")
	require.Equal(t, before.CheckoutID, blocked.CheckoutID)
	var run string
	require.NoError(t, f.db.Get(&run, `SELECT status::text FROM dev_machine_agent_runs WHERE id=$1`, before.RunID))
	require.Equal(t, "cancelled", run)
	require.Nil(t, f.claim(), "the worker leaves a paused task alone")

	// Only the creator pauses and resumes, as only they cancel.
	admin := addWorkflowMember(t, f.db, f.ws, domain.RoleAdmin)
	member := addWorkflowMember(t, f.db, f.ws, domain.RoleMember)
	for _, other := range []uuid.UUID{admin, member, uuid.New()} {
		ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other})
		_, err = f.repo.ResumeWorkflow(ctx, f.ws, f.task.ID)
		require.ErrorIs(t, err, ErrConversationNotFound)
		_, err = f.repo.PauseWorkflow(ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
		require.ErrorIs(t, err, ErrConversationNotFound)
		_, err = f.repo.WorkflowPauses(ctx, f.ws, f.task.ID)
		require.ErrorIs(t, err, ErrConversationNotFound)
	}
	// Nor does anyone from another workspace, whatever ID they send.
	elsewhere := uuid.New()
	foreign := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: elsewhere, UserID: before.CreatorID})
	_, err = f.repo.ResumeWorkflow(foreign, elsewhere, f.task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = f.repo.ResumeWorkflow(foreign, f.ws, f.task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = f.repo.PauseWorkflow(foreign, elsewhere, f.task.ID, domain.WorkflowPauseNow)
	require.ErrorIs(t, err, ErrConversationNotFound)
	f.exec(`UPDATE agent_tasks SET workflow_paused_at=now()-interval '2 hours' WHERE id=$1`, f.task.ID)
	resumed, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", resumed.State)
	require.Equal(t, "Resumed", resumed.Reason)
	require.Nil(t, resumed.WorkflowPausedAt)
	require.Equal(t, before.Generation+1, resumed.Generation)
	require.NotEqual(t, before.RunID, resumed.RunID)
	require.True(t, resumed.ResumeNativeSession, "the new attempt continues the stopped attempt's session")
	require.Equal(t, before.WorkflowSequence, resumed.WorkflowSequence)
	require.Equal(t, *before.WorkflowNode, *resumed.WorkflowNode)
	require.JSONEq(t, string(before.WorkflowVisits), string(resumed.WorkflowVisits))
	require.Equal(t, before.WorkflowIterations, resumed.WorkflowIterations)
	require.Equal(t, before.Prompt, resumed.Prompt)
	require.NotNil(t, resumed.WorkflowBudgetFrom)
	require.WithinDuration(t, before.WorkflowStartedAt.Add(2*time.Hour), *resumed.WorkflowBudgetFrom, 5*time.Second, "the time budget starts two hours later")
	require.WithinDuration(t, *before.WorkflowStartedAt, *resumed.WorkflowStartedAt, time.Millisecond)
	step := f.step(resumed.WorkflowSequence)
	require.EqualValues(t, resumed.Generation, *step.Generation)
	require.Nil(t, step.FinishedAt)
	require.InDelta(t, 7200, step.PausedSeconds, 5)
	var steps int
	require.NoError(t, f.db.Get(&steps, `SELECT count(*) FROM agent_task_workflow_steps WHERE task_id=$1`, f.task.ID))
	require.Equal(t, 2, steps, "resuming enters no step")
	handoff, err := f.repo.PreviousAttempt(f.ctx, *resumed)
	require.NoError(t, err)
	require.Equal(t, before.RunID, handoff.AttemptID)
	require.Equal(t, "cancelled", handoff.Status)
	require.False(t, handoff.OtherProfile)

	_, err = f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	require.NotNil(t, f.claim(), "the worker runs the task again")

	events, err := f.repo.WorkflowPauses(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, domain.WorkflowPauseActionPaused, events[0].Action)
	require.Equal(t, domain.WorkflowPauseNow, events[0].Mode)
	require.Equal(t, before.WorkflowSequence, events[0].Sequence)
	require.Equal(t, before.CreatorID, *events[0].ActorID)
	require.Equal(t, domain.WorkflowPauseActionResumed, events[1].Action)
	require.False(t, events[1].At.Before(events[0].At))

	// An attempt stopped before it started has no session to continue.
	f.exec(`UPDATE agent_tasks SET state='blocked',workflow_paused_at=now() WHERE id=$1`, f.task.ID)
	again, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.False(t, again.ResumeNativeSession)
}

// A waiting step pauses where it is: nothing wakes it, nobody decides it,
// and after a resume it is evaluated at once.
func TestWorkflowPauseHoldsAWaitingStep(t *testing.T) {
	f := newPauseFixture(t)
	moved, err := f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(f.current()))
	require.NoError(t, err)
	publish, _ := f.graph.Node("publish")
	ci, _ := f.graph.Node("ci")
	wake := time.Now().Add(time.Hour)
	f.claimed.WorkflowSequence = moved.WorkflowSequence
	waiting, err := f.repo.ApplyWorkflow(f.ctx, *f.claimed, WorkflowApply{Sequence: moved.WorkflowSequence, Outcome: domain.WorkflowPublished,
		Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: publish, To: ci, Reason: "Open pull request → CI"}}},
		Entry: WorkflowEntry{Kind: WorkflowEnterWait, Reason: "Workflow · waiting for CI on the pull request", WakeAt: &wake}})
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", waiting.State)
	require.NoError(t, f.repo.Release(f.ctx, *f.claimed))

	paused, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", paused.State, "nothing runs, so nothing is stopped")
	require.NotNil(t, paused.WorkflowPausedAt)
	require.Nil(t, paused.WorkflowPauseRequest)
	require.Equal(t, waiting.Reason, paused.Reason)

	// CI finishes: its webhook wakes the step, and the worker still does not
	// claim the task.
	f.exec(`UPDATE agent_tasks SET workflow_wake_at=now()-interval '1 second' WHERE id=$1`, f.task.ID)
	require.Nil(t, f.claim())
	// A person's decision is refused too.
	failed, _ := f.graph.Node("failed")
	stop := WorkflowApply{Sequence: waiting.WorkflowSequence, Outcome: domain.WorkflowChecksPassed, Moves: []WorkflowMove{{Decision: domain.WorkflowDecision{From: ci, To: failed, Reason: "CI → Failed"}}},
		Entry: WorkflowEntry{Kind: WorkflowEnterFail, Reason: "Workflow · stopped"}}
	_, err = f.repo.DecideWorkflow(f.ctx, f.ws, f.task.ID, stop)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	require.Nil(t, f.step(waiting.WorkflowSequence).FinishedAt)

	f.exec(`UPDATE agent_tasks SET workflow_paused_at=now()-interval '90 minutes',workflow_wake_at=now()+interval '1 hour' WHERE id=$1`, f.task.ID)
	resumed, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Equal(t, "awaiting_review", resumed.State)
	require.Equal(t, waiting.Generation, resumed.Generation, "no attempt is started")
	require.Equal(t, waiting.WorkflowSequence, resumed.WorkflowSequence)
	require.InDelta(t, 5400, f.step(waiting.WorkflowSequence).PausedSeconds, 5)
	require.WithinDuration(t, waiting.WorkflowStartedAt.Add(90*time.Minute), *resumed.WorkflowBudgetFrom, 5*time.Second)
	claimed := f.claim()
	require.NotNil(t, claimed, "the step is evaluated as soon as the workflow resumes")

	// Cancelling a paused workflow cancels it.
	require.NoError(t, f.repo.Release(f.ctx, *claimed))
	_, err = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
	require.NoError(t, err)
	require.NoError(t, f.repo.RequestCancel(f.ctx, f.ws, f.task.ID))
	cancelling := f.current()
	require.Equal(t, "cancelling", cancelling.State)
	require.Nil(t, cancelling.WorkflowPausedAt)
	f.cleanup()
	cancelled := f.current()
	require.Equal(t, "cancelled", cancelled.State)
	require.Equal(t, domain.WorkflowStatusFailed, *cancelled.WorkflowStatus)
	_, err = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
}

// "Pause after this step" lets the agent finish: the pause takes effect when
// the worker has the step's outcome, before the next step is entered.
func TestWorkflowPauseAfterThisStepParksBeforeTheNextStep(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()

	// A request can be withdrawn before it takes effect.
	requested, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	require.Equal(t, "running", requested.State)
	require.Equal(t, domain.WorkflowPauseAfterStep, *requested.WorkflowPauseRequest)
	require.Nil(t, requested.WorkflowPausedAt)
	_, err = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseAfterStep)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	withdrawn, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Nil(t, withdrawn.WorkflowPauseRequest)
	require.Equal(t, before.Generation, withdrawn.Generation)

	_, err = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	// The agent finishes. The worker that claimed the task before the request
	// cannot move it any more; it claims again and sees the request.
	f.exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0 WHERE id=$1`, before.RunID)
	_, err = f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(before))
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	claimed := f.claim()
	require.NotNil(t, claimed)
	require.NotNil(t, claimed.WorkflowPauseRequest)
	require.NoError(t, f.repo.ParkWorkflow(f.ctx, *claimed))
	require.ErrorIs(t, f.repo.ParkWorkflow(f.ctx, *claimed), ErrAgentTaskConflict)
	parked := f.current()
	require.Equal(t, "running", parked.State)
	require.NotNil(t, parked.WorkflowPausedAt)
	require.Nil(t, parked.WorkflowPauseRequest)
	require.Equal(t, before.WorkflowSequence, parked.WorkflowSequence)
	require.Nil(t, f.step(before.WorkflowSequence).FinishedAt)
	_, err = f.repo.ApplyWorkflow(f.ctx, *claimed, f.toPublish(parked))
	require.ErrorIs(t, err, ErrAgentTaskConflict, "no step starts while paused")
	require.Nil(t, f.claim())

	resumed, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Equal(t, "running", resumed.State)
	require.Equal(t, before.Generation, resumed.Generation, "the finished attempt is not run again")
	require.Equal(t, before.RunID, resumed.RunID)
	claimed = f.claim()
	require.NotNil(t, claimed)
	moved, err := f.repo.ApplyWorkflow(f.ctx, *claimed, f.toPublish(*claimed))
	require.NoError(t, err)
	require.Equal(t, "publishing", moved.State)
	require.Equal(t, before.WorkflowIterations, moved.WorkflowIterations)

	// A publication is Kuayle's own work: "pause now" waits for it.
	held, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
	require.NoError(t, err)
	require.Equal(t, "publishing", held.State)
	require.Equal(t, domain.WorkflowPauseNow, *held.WorkflowPauseRequest)

	events, err := f.repo.WorkflowPauses(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	var actions []string
	for _, event := range events {
		actions = append(actions, event.Action)
	}
	require.Equal(t, []string{"pause_requested", "pause_withdrawn", "pause_requested", "paused", "resumed", "pause_requested"}, actions)
	require.Nil(t, events[3].ActorID, "the request took effect by itself")
	require.Equal(t, domain.WorkflowPauseAfterStep, events[3].Mode)

	// A retry is the creator's own way out of a pause.
	f.exec(`UPDATE agent_tasks SET state='blocked',workflow_pause_request=NULL,workflow_paused_at=now()-interval '10 minutes' WHERE id=$1`, f.task.ID)
	f.exec(`UPDATE dev_machine_operations SET status='completed' WHERE status IN ('pending','leased')`)
	retried, err := f.repo.Retry(f.ctx, f.ws, f.task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: moved.Generation, Prompt: "Try again"})
	require.NoError(t, err)
	require.Equal(t, "queued", retried.State)
	require.Nil(t, retried.WorkflowPausedAt)
	require.NotNil(t, f.claim())
}

// A pause and the step that finishes at the same moment never both win: the
// step is applied and the pause waits for the next one, or the pause stops
// the attempt and the step is refused.
func TestWorkflowPauseRacesAFinishingStep(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	var wg sync.WaitGroup
	var applyErr, pauseErr error
	wg.Go(func() { _, applyErr = f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(before)) })
	wg.Go(func() { _, pauseErr = f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow) })
	wg.Wait()
	require.NoError(t, pauseErr)
	after := f.current()
	if applyErr == nil {
		require.Equal(t, "publishing", after.State)
		require.Equal(t, before.WorkflowSequence+1, after.WorkflowSequence)
		require.Nil(t, after.WorkflowPausedAt)
		require.Equal(t, domain.WorkflowPauseNow, *after.WorkflowPauseRequest, "the pause waits for the publication")
		return
	}
	require.ErrorIs(t, applyErr, ErrAgentTaskConflict)
	require.Equal(t, "cancelling", after.State)
	require.Equal(t, before.WorkflowSequence, after.WorkflowSequence)
	require.NotNil(t, after.WorkflowPausedAt)
	require.Nil(t, f.step(before.WorkflowSequence).FinishedAt)

	// Cancelling while the pause is still stopping the attempt cancels.
	require.NoError(t, f.repo.RequestCancel(f.ctx, f.ws, f.task.ID))
	f.cleanup()
	require.Equal(t, "cancelled", f.current().State)
}

// Cancelling wins over a pause, whether the pause is still stopping the
// attempt or already holds the task.
func TestCancellingAPausedWorkflowCancelsIt(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		f := newPauseFixture(t)
		_, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
		require.NoError(t, err)
		if stopped {
			f.cleanup()
			require.Equal(t, "blocked", f.current().State)
		}
		require.NoError(t, f.repo.RequestCancel(f.ctx, f.ws, f.task.ID))
		cancelling := f.current()
		require.Equal(t, "cancelling", cancelling.State)
		require.Equal(t, "Cancellation requested", cancelling.Reason)
		require.Nil(t, cancelling.WorkflowPausedAt)
		f.cleanup()
		cancelled := f.current()
		require.Equal(t, "cancelled", cancelled.State)
		require.Equal(t, domain.WorkflowStatusFailed, *cancelled.WorkflowStatus)
		_, err = f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
		require.ErrorIs(t, err, ErrAgentTaskConflict)
	}
}

// A task in a private team is paused and resumed only by its creator while
// they can see the team: not by other members, admins or the owner outside
// it, and not by the creator once they left it.
func TestWorkflowPauseKeepsPrivateTeamsPrivate(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	owner, creator, member, admin, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{owner, creator, member, admin} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Pause test','hash')`, id, id.String()+"@test.invalid")
	}
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Pause test',$2,$3)`, ws, ws.String(), owner)
	for id, role := range map[uuid.UUID]string{owner: "owner", creator: "member", member: "member", admin: "admin"} {
		exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
	}
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Private','PRV',true)`, team, ws)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, creator)
	exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id) VALUES($1,$2,$3,1,'PRV-1','Private issue',$4,'todo',(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, creator)
	as := func(user uuid.UUID) context.Context {
		return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	}
	chat, err := NewConversationRepository(db).OpenIssue(as(creator), ws, issue)
	require.NoError(t, err)
	repo := NewAgentTaskRepository(db)
	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	raw, err := json.Marshal(domain.ReviewLoopWorkflowGraph(implementer, reviewer))
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(as(owner), ws, owner, "Review loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(as(creator), ws, issue, &chosen)
	require.NoError(t, err)
	task, err := repo.Create(as(creator), ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement", Provider: "claude-code", Workflow: version})
	require.NoError(t, err)
	// A pause is pending, so a caller who got through would change the task.
	exec(`UPDATE agent_tasks SET state='running',workflow_pause_request='after_step' WHERE id=$1`, task.ID)
	// Its path has a move to a newer version, which nobody else reads either.
	exec(`INSERT INTO workflow_audit_events(workspace_id,actor_id,entity,entity_id,action,detail) VALUES($1,$2,'task_workflow',$3,$4,'{"from_version":1,"to_version":2,"sequence":1}')`, ws, creator, task.ID, domain.WorkflowSwitchAction)
	for _, other := range []uuid.UUID{member, admin, owner} {
		_, err = repo.PauseWorkflow(as(other), ws, task.ID, domain.WorkflowPauseNow)
		require.ErrorIs(t, err, ErrConversationNotFound)
		_, err = repo.ResumeWorkflow(as(other), ws, task.ID)
		require.ErrorIs(t, err, ErrConversationNotFound)
		_, err = repo.WorkflowPauses(as(other), ws, task.ID)
		require.ErrorIs(t, err, ErrConversationNotFound)
	}
	moves, err := repo.WorkflowPauses(as(creator), ws, task.ID)
	require.NoError(t, err)
	require.Len(t, moves, 1)
	require.Equal(t, 2, moves[0].ToVersion)
	exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, creator)
	_, err = repo.WorkflowPauses(as(creator), ws, task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.ResumeWorkflow(as(creator), ws, task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.PauseWorkflow(as(creator), ws, task.ID, domain.WorkflowPauseNow)
	require.ErrorIs(t, err, ErrConversationNotFound)
	var request *string
	require.NoError(t, db.Get(&request, `SELECT workflow_pause_request FROM agent_tasks WHERE id=$1`, task.ID))
	require.NotNil(t, request, "nobody outside the team changed the task")

	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, creator)
	resumed, err := repo.ResumeWorkflow(as(creator), ws, task.ID)
	require.NoError(t, err)
	require.Nil(t, resumed.WorkflowPauseRequest)
}

// "After this step" was requested and then the agent hit its usage limit:
// the task is paused by the limit with the request still pending. One resume
// by its creator starts it again; it does not merely withdraw the request.
func TestResumeAfterAUsageLimitWithAPauseRequested(t *testing.T) {
	f := newPauseFixture(t)
	_, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	claimed := f.claim()
	require.NotNil(t, claimed)
	limit := domain.UsageLimit{Message: "You've hit your usage limit. Try again in 1 hour."}
	require.NoError(t, f.repo.PauseForUsageLimit(f.ctx, *claimed, limit, "Paused: usage limit reached.", "Waiting for the usage limit to reset"))
	f.cleanup()
	limited := f.current()
	require.Equal(t, "blocked", limited.State)
	require.NotNil(t, limited.UsageLimitMessage)
	require.NotNil(t, limited.WorkflowPauseRequest)

	resumed, err := f.repo.ResumePaused(f.ctx, f.ws, f.task.ID, "Resumed by request after the usage limit")
	require.NoError(t, err)
	require.Equal(t, "queued", resumed.State, "the task runs again")
	require.Nil(t, resumed.UsageLimitMessage)
	require.Nil(t, resumed.WorkflowPauseRequest, "resuming is the creator's decision to go on")
	require.True(t, resumed.ResumeNativeSession)
	require.NotNil(t, f.claim())

	// A workflow its creator paused resumes through the same call.
	f.exec(`UPDATE agent_tasks SET state='awaiting_review',workflow_status='waiting',workflow_paused_at=now() WHERE id=$1`, f.task.ID)
	resumed, err = f.repo.ResumePaused(f.ctx, f.ws, f.task.ID, "unused")
	require.NoError(t, err)
	require.Nil(t, resumed.WorkflowPausedAt)
	_, err = f.repo.ResumePaused(f.ctx, f.ws, f.task.ID, "unused")
	require.ErrorIs(t, err, ErrAgentTaskConflict, "nothing is paused")
}

// The cleanup of the attempt "pause now" stopped can fail for good (the
// machine manager is down). The worker starts it again, and the task ends
// paused and resumable when it succeeds; its creator can still cancel it.
func TestFailedPauseCleanupIsRetriedAndEndsPaused(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		f := newPauseFixture(t)
		_, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
		require.NoError(t, err)
		claimed := f.claim()
		require.NoError(t, f.repo.CancelAttempt(f.ctx, *claimed))
		operation := func() (status string, attempts int) {
			require.NoError(t, f.db.QueryRow(`SELECT status::text,attempts FROM dev_machine_operations WHERE agent_run_id=$1 AND action='cancel_agent'`, claimed.RunID).Scan(&status, &attempts))
			return status, attempts
		}
		f.exec(`UPDATE dev_machine_operations SET status='failed',attempts=5,error_code='manager_down' WHERE agent_run_id=$1 AND action='cancel_agent'`, claimed.RunID)
		_, err = f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
		require.ErrorIs(t, err, ErrAgentTaskConflict)

		if cancel {
			require.NoError(t, f.repo.RequestCancel(f.ctx, f.ws, f.task.ID))
			status, attempts := operation()
			require.Equal(t, "pending", status, "cancelling retries the cleanup as it always does")
			require.Zero(t, attempts)
			f.cleanup()
			require.Equal(t, "cancelled", f.current().State)
			continue
		}
		claimed = f.claim()
		require.NotNil(t, claimed)
		require.NoError(t, f.repo.CancelAttempt(f.ctx, *claimed), "the worker starts the failed cleanup again")
		status, attempts := operation()
		require.Equal(t, "pending", status)
		require.Zero(t, attempts)
		require.ErrorIs(t, f.repo.FinishCancellation(f.ctx, *claimed, "Runtime cancellation confirmed"), ErrAgentTaskConflict, "not before the cleanup is done")
		f.cleanup()
		paused := f.current()
		require.Equal(t, "blocked", paused.State)
		require.Equal(t, "Paused", paused.Reason)
		require.NotNil(t, paused.WorkflowPausedAt)
		resumed, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
		require.NoError(t, err)
		require.Equal(t, "queued", resumed.State)

		// A cleanup that is not a pause's still waits for an explicit retry.
		require.NoError(t, f.repo.RequestCancel(f.ctx, f.ws, f.task.ID))
		claimed = f.claim()
		f.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at) VALUES($1,$2,$3,$4,$5,'codex','autonomous','Test','["codex"]',60,'running',now())`, claimed.RunID, claimed.WorkspaceID, *claimed.MachineID, claimed.ID, claimed.CreatorID)
		require.NoError(t, f.repo.CancelAttempt(f.ctx, *claimed))
		f.exec(`UPDATE dev_machine_operations SET status='failed',attempts=5 WHERE agent_run_id=$1 AND action='cancel_agent'`, claimed.RunID)
		require.ErrorIs(t, f.repo.CancelAttempt(f.ctx, *claimed), ErrAgentTaskCleanupFailed)
	}
}

// A paused task holds no place on its machine, however it was paused: other
// tasks reserve the machine and start their attempts there. The cleanup of
// an attempt a pause stopped is still work.
func TestAPausedTaskIsNotMachineLoad(t *testing.T) {
	f := newMachineShareFixture(t)
	f.taskLimit(t, 1)
	machine := f.runningMachine(t, "1 minute")
	parked := f.workingTask(t, machine)
	f.exec(t, `UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE task_id=$1`, parked)
	claimed := f.preparingTask(t)
	reserve := func() (uuid.UUID, error) {
		t.Helper()
		f.exec(t, `UPDATE agent_tasks SET machine_id=NULL WHERE id=$1`, claimed.ID)
		claimed.MachineID = nil
		reservation, err := f.repo.ReserveMachine(f.ctx, claimed, uuid.New(), &f.environmentID)
		return reservation.MachineID, err
	}
	start := func() error {
		f.exec(t, `UPDATE agent_tasks SET machine_id=$2 WHERE id=$1`, claimed.ID, machine)
		at := claimed
		at.MachineID = &machine
		run, op := taskRun(at, machine)
		return f.repository.CreateAgentRun(domain.WithTaskExecution(f.ctx, at), run, op)
	}
	_, err := reserve()
	require.ErrorIs(t, err, ErrMachineSlotWait, "a task between two steps waits for the machine's only task slot")
	require.ErrorIs(t, start(), ErrMachineBusy)
	f.exec(t, `UPDATE agent_tasks SET state='cancelling',workflow_paused_at=now() WHERE id=$1`, parked)
	_, err = reserve()
	require.ErrorIs(t, err, ErrMachineSlotWait, "stopping the attempt is still work")
	for _, state := range []string{"validating", "publishing", "running"} {
		f.exec(t, `UPDATE agent_tasks SET state=$2 WHERE id=$1`, parked, state)
		id, err := reserve()
		require.NoError(t, err, state)
		require.Equal(t, machine, id, state)
	}
	require.NoError(t, start(), "the paused task's place is free")
}

// The workflow API lists the newest pauses and resumes, oldest first.
func TestWorkflowPausesAreTheNewest(t *testing.T) {
	f := newPauseFixture(t)
	f.exec(`INSERT INTO workflow_audit_events(workspace_id,actor_id,entity,entity_id,action,detail)
 SELECT $1,$2,'task_workflow',$3,CASE WHEN n%2=1 THEN 'paused' ELSE 'resumed' END,jsonb_build_object('sequence',n) FROM generate_series(1,620) n`, f.ws, f.task.CreatorID, f.task.ID)
	events, err := f.repo.WorkflowPauses(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Len(t, events, 500)
	require.Equal(t, 121, events[0].Sequence)
	require.Equal(t, 620, events[len(events)-1].Sequence)
}

// A move to a newer version of the workflow is listed with the pauses, where
// it happened, with the versions it went from and to; only the task's
// creator reads it.
func TestWorkflowPausesListAMoveToANewerVersion(t *testing.T) {
	f := newPauseFixture(t)
	_, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	f.exec(`INSERT INTO workflow_audit_events(workspace_id,actor_id,entity,entity_id,action,detail) VALUES($1,$2,'task_workflow',$3,$4,$5)`,
		f.ws, f.task.CreatorID, f.task.ID, domain.WorkflowSwitchAction, `{"from_version":1,"to_version":3,"node":"implement","sequence":2}`)
	// Another task's move, and another kind of event, are not this task's path.
	f.exec(`INSERT INTO workflow_audit_events(workspace_id,actor_id,entity,entity_id,action,detail) VALUES($1,$2,'task_workflow',$3,$4,'{"from_version":1,"to_version":2,"sequence":1}')`,
		f.ws, f.task.CreatorID, uuid.New(), domain.WorkflowSwitchAction)
	f.exec(`INSERT INTO workflow_audit_events(workspace_id,actor_id,entity,entity_id,action,detail) VALUES($1,$2,'workflow',$3,'updated','{}')`, f.ws, f.task.CreatorID, f.task.ID)
	events, err := f.repo.WorkflowPauses(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, domain.WorkflowPauseActionRequested, events[0].Action)
	require.Zero(t, events[0].ToVersion)
	move := events[1]
	require.Equal(t, domain.WorkflowSwitchAction, move.Action)
	require.Equal(t, []int{1, 3, 2}, []int{move.FromVersion, move.ToVersion, move.Sequence})
	require.Equal(t, f.task.CreatorID, *move.ActorID)
	require.False(t, move.At.Before(events[0].At))
	raw, err := json.Marshal(move)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"from_version":1,"to_version":3`)
	pause, err := json.Marshal(events[0])
	require.NoError(t, err)
	require.NotContains(t, string(pause), "to_version")

	// Not a workspace admin, not another member, not another workspace.
	for _, other := range []uuid.UUID{addWorkflowMember(t, f.db, f.ws, domain.RoleAdmin), addWorkflowMember(t, f.db, f.ws, domain.RoleMember), uuid.New()} {
		_, err = f.repo.WorkflowPauses(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other}), f.ws, f.task.ID)
		require.ErrorIs(t, err, ErrConversationNotFound)
	}
	elsewhere := uuid.New()
	foreign := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: elsewhere, UserID: f.task.CreatorID})
	_, err = f.repo.WorkflowPauses(foreign, elsewhere, f.task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = f.repo.WorkflowPauses(foreign, f.ws, f.task.ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
}
