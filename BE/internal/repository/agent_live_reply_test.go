package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Talking to the running agent (KUA-132), on the pause fixture: a task on the
// review-loop workflow whose implementing agent is running.

func (f *pauseFixture) send(body, mode string) (*domain.AgentLiveMessage, error) {
	return f.repo.SendLiveMessage(f.ctx, f.ws, f.task.ID, body, mode)
}

func (f *pauseFixture) liveMessages() []domain.AgentLiveMessage {
	f.t.Helper()
	messages, err := f.repo.LiveMessages(f.ctx, f.ws, f.task.ID, 100)
	require.NoError(f.t, err)
	return messages
}

// deliver writes the task's queued messages to its current attempt's inbox,
// as the machine manager does, some time ago.
func (f *pauseFixture) deliver() {
	f.t.Helper()
	machines := NewDevMachineRepository(f.db)
	run := f.current().RunID
	pending, err := machines.PendingLiveMessages(f.ctx, run)
	require.NoError(f.t, err)
	ids := []uuid.UUID{}
	for _, message := range pending {
		ids = append(ids, message.ID)
	}
	marked, err := machines.MarkLiveMessagesDelivered(f.ctx, run, ids)
	require.NoError(f.t, err)
	require.Equal(f.t, len(ids), marked)
	f.exec(`UPDATE agent_live_messages SET delivered_at=now()-interval '5 minutes' WHERE task_id=$1 AND run_id=$2`, f.task.ID, run)
}

// nextAttempt ends the task's current attempt with status and gives the task
// a new one on the same step, as a retry or a resume does. The new attempt's
// run is recorded when record is set.
func (f *pauseFixture) nextAttempt(status string, record bool) domain.AgentTask {
	f.t.Helper()
	before := f.current()
	// status is one of the run statuses, written by the tests themselves.
	f.exec(`UPDATE dev_machine_agent_runs SET status='`+status+`',completed_at=now() WHERE id=$1`, before.RunID)
	f.exec(`UPDATE agent_tasks SET run_id=gen_random_uuid(),generation=generation+1 WHERE id=$1`, f.task.ID)
	next := f.current()
	if record {
		f.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,started_at,created_at)
 VALUES($1,$2,$3,$4,$5,'claude-code','autonomous','Test','["claude"]',60,'running',now(),clock_timestamp())`, next.RunID, next.WorkspaceID, next.MachineID, next.ID, next.CreatorID)
	}
	return next
}

func (f *pauseFixture) unanswered(run uuid.UUID) []int64 {
	f.t.Helper()
	seqs, err := NewDevMachineRepository(f.db).UnansweredLiveMessages(f.ctx, run)
	require.NoError(f.t, err)
	return seqs
}

// A delivered message is owed a reply until the agent writes an outbox file
// that names its sequence number; nothing else counts.
func TestOnlyAnOutboxFileNamingTheMessageIsItsReply(t *testing.T) {
	f := newPauseFixture(t)
	outboxes := NewAgentMessageRepository(f.db)
	run := f.current().RunID
	sent, err := f.send("Keep the v1 route", "")
	require.NoError(t, err)
	require.Equal(t, domain.AgentLiveQueued, sent.State)
	require.Zero(t, sent.Deliveries)
	require.Equal(t, "running", f.current().State, "Claude Code reads its inbox: nothing is interrupted")
	require.Empty(t, f.unanswered(run), "a message that was not delivered owes nothing yet")

	f.deliver()
	delivered := f.liveMessages()[0]
	require.Equal(t, domain.AgentLiveDelivered, delivered.State)
	require.Equal(t, 1, delivered.Deliveries)
	require.Equal(t, []int64{1}, f.unanswered(run))
	require.Empty(t, f.unanswered(uuid.New()), "only the attempt that received it")

	recorded, err := outboxes.RecordTaskOutbox(f.ctx, f.task.ID, []domain.AgentOutboxFile{
		outbox("report.json", `{"to":"orchestrator","body":"Keeping the v1 route."}`),
		outbox("wrong.json", `{"to":"orchestrator","reply_to":9,"body":"Understood."}`),
	})
	require.NoError(t, err)
	require.Equal(t, 2, recorded)
	require.Equal(t, []int64{1}, f.unanswered(run), "a report without reply_to, or naming no message, is not a reply")
	var reason string
	require.NoError(t, f.db.Get(&reason, `SELECT reject_reason FROM agent_live_messages WHERE task_id=$1 AND reply_to=9`, f.task.ID))
	require.Equal(t, "reply_to does not name a message this task received", reason)

	_, err = outboxes.RecordTaskOutbox(f.ctx, f.task.ID, []domain.AgentOutboxFile{
		outbox("reply.json", `{"to":"orchestrator","reply_to":1,"body":"Understood: the v1 route stays; I am reverting its removal."}`),
	})
	require.NoError(t, err)
	require.Empty(t, f.unanswered(run))
	var reply domain.AgentLiveMessage
	for _, message := range f.liveMessages() {
		if message.ReplyTo != nil && message.State == domain.AgentLiveReceived {
			reply = message
		}
	}
	require.Equal(t, int64(1), *reply.ReplyTo)
	require.Equal(t, domain.AgentLiveFromAgent, reply.Direction)
	require.Equal(t, run, *reply.RunID)
	settled, err := NewDevMachineRepository(f.db).SettleLiveMessages(f.ctx, run, 0)
	require.NoError(t, err)
	require.Empty(t, settled, "the attempt may finish")
}

// An attempt that ends owing a reply is told (SettleLiveMessages names the
// message; the manager fails the attempt). The message is then given to the
// same step's next attempt once more, through its prompt, and never again
// after that, nor to another step.
func TestAnUnansweredMessageIsGivenToTheSameStepOnceMore(t *testing.T) {
	f := newPauseFixture(t)
	machines := NewDevMachineRepository(f.db)
	first := f.current()
	_, err := f.send("Keep the v1 route", "")
	require.NoError(t, err)
	f.deliver()
	// A message written to the inbox in the attempt's last seconds may not
	// have been read: it is queued again instead of counting as delivered.
	_, err = f.send("Use the shared client", "")
	require.NoError(t, err)
	pending, err := machines.PendingLiveMessages(f.ctx, first.RunID)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	_, err = machines.MarkLiveMessagesDelivered(f.ctx, first.RunID, []uuid.UUID{pending[0].ID})
	require.NoError(t, err)
	settled, err := machines.SettleLiveMessages(f.ctx, first.RunID, 20*time.Second)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, settled, "the attempt owes a reply to the message it had time to read")
	late := f.liveMessages()[1]
	require.Equal(t, domain.AgentLiveQueued, late.State)
	require.Zero(t, late.Deliveries)
	require.Nil(t, late.RunID)

	// The retry is given both: the unanswered one again, the late one for
	// the first time.
	second := f.nextAttempt("failed", true)
	claimed, err := f.repo.ClaimLiveMessages(f.ctx, second, 10000)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, "Keep the v1 route", claimed[0].Body)
	require.Equal(t, 2, claimed[0].Deliveries)
	require.Equal(t, domain.AgentLiveViaPrompt, *claimed[0].DeliveredVia)
	require.Equal(t, second.RunID, *claimed[0].RunID)
	require.Equal(t, 1, claimed[1].Deliveries)
	again, err := f.repo.ClaimLiveMessages(f.ctx, second, 10000)
	require.NoError(t, err)
	require.Len(t, again, 2, "recreating the attempt places the same messages")
	require.Equal(t, 2, again[0].Deliveries, "and is still the same delivery")
	require.Equal(t, []int64{1, 2}, f.unanswered(second.RunID), "a message delivered through the prompt is owed a reply too")

	// The retry answers the late message only. Kuayle stops asking for the
	// first: it stays unanswered, and no later attempt is given it.
	_, err = NewAgentMessageRepository(f.db).RecordTaskOutbox(f.ctx, f.task.ID, []domain.AgentOutboxFile{
		outbox("reply.json", `{"to":"orchestrator","reply_to":2,"body":"Understood: switching to the shared client."}`),
	})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, f.unanswered(second.RunID))
	third := f.nextAttempt("failed", true)
	claimed, err = f.repo.ClaimLiveMessages(f.ctx, third, 10000)
	require.NoError(t, err)
	require.Empty(t, claimed, "a message is given to two attempts at most")
	require.Empty(t, f.unanswered(third.RunID))

	// A message the step left unanswered is not carried to another step.
	_, err = f.send("Rename the flag", "")
	require.NoError(t, err)
	f.deliver()
	require.Equal(t, []int64{3}, f.unanswered(third.RunID))
	f.exec(`UPDATE agent_tasks SET workflow_node='review' WHERE id=$1`, f.task.ID)
	other := f.nextAttempt("failed", true)
	claimed, err = f.repo.ClaimLiveMessages(f.ctx, other, 10000)
	require.NoError(t, err)
	require.Empty(t, claimed, "a message goes to the step that received it only")
}

// Interrupt and send is one action: the working attempt is stopped through
// the pause-now cleanup, and when it is gone the step continues by itself as
// a new attempt on the same session, with the message in its prompt.
func TestInterruptAndSendStopsTheAgentAndResumesItsSession(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	sent, err := f.send("Use the shared client", domain.AgentLiveSendInterrupt)
	require.NoError(t, err)
	require.Equal(t, domain.AgentLiveQueued, sent.State)
	stopping := f.current()
	require.Equal(t, "cancelling", stopping.State)
	require.Equal(t, liveInterruptingReason, stopping.Reason)
	require.NotNil(t, stopping.WorkflowPausedAt)
	require.False(t, stopping.FailurePending, "nothing failed")
	require.False(t, stopping.LiveResumed(), "the attempt it will resume as does not exist yet")
	pending, err := NewDevMachineRepository(f.db).PendingLiveMessages(f.ctx, before.RunID)
	require.NoError(t, err)
	require.Empty(t, pending, "the attempt being stopped does not receive the message")

	// A step the worker was finishing when the interruption arrived is refused.
	_, err = f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(before))
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	_, err = f.send("Start", domain.AgentLiveSendStart)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the attempt is still being stopped")
	require.Len(t, f.liveMessages(), 1, "a refused send queues nothing")
	// A second message sent meanwhile joins the first: the agent is not
	// interrupted twice.
	also, err := f.send("And keep the v1 route", domain.AgentLiveSendInterrupt)
	require.NoError(t, err)
	require.Equal(t, "cancelling", f.current().State)
	require.True(t, stopping.WorkflowPausedAt.Equal(*f.current().WorkflowPausedAt), "the same interruption")

	f.cleanup()
	resumed := f.current()
	require.Equal(t, "queued", resumed.State, "the person does not resume it themselves")
	require.Equal(t, liveInterruptedReason, resumed.Reason)
	require.Nil(t, resumed.WorkflowPausedAt)
	require.Equal(t, before.Generation+1, resumed.Generation)
	require.NotEqual(t, before.RunID, resumed.RunID)
	require.True(t, resumed.ResumeNativeSession, "the new attempt continues the interrupted session")
	require.True(t, resumed.LiveResumed())
	require.Equal(t, before.WorkflowSequence, resumed.WorkflowSequence, "the same step")
	require.JSONEq(t, string(before.WorkflowVisits), string(resumed.WorkflowVisits))
	require.Equal(t, before.WorkflowIterations, resumed.WorkflowIterations)
	require.Equal(t, before.MachineID, resumed.MachineID, "the machine, the checkout and the worktree stay the task's")
	require.Equal(t, before.CheckoutID, resumed.CheckoutID)
	require.Equal(t, before.Prompt, resumed.Prompt)
	var run string
	require.NoError(t, f.db.Get(&run, `SELECT status::text FROM dev_machine_agent_runs WHERE id=$1`, before.RunID))
	require.Equal(t, "cancelled", run)
	handoff, err := f.repo.PreviousAttempt(f.ctx, resumed)
	require.NoError(t, err)
	require.Equal(t, before.RunID, handoff.AttemptID)
	require.Equal(t, "cancelled", handoff.Status)

	claimed, err := f.repo.ClaimLiveMessages(f.ctx, resumed, 10000)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, sent.ID, claimed[0].ID)
	require.Equal(t, also.ID, claimed[1].ID)
	require.Equal(t, resumed.RunID, *claimed[0].RunID)
	require.Equal(t, 1, claimed[0].Deliveries)
	require.NotNil(t, f.claim(), "the worker runs the task again")

	events, err := f.repo.WorkflowPauses(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Len(t, events, 2, "the interruption is audited as a pause and its resume")
	require.Equal(t, domain.WorkflowPauseActionPaused, events[0].Action)
	require.Equal(t, before.CreatorID, *events[0].ActorID)
	require.Equal(t, domain.WorkflowPauseActionResumed, events[1].Action)
}

// A resume that cannot happen leaves the task paused and saying why; the
// message is delivered when the person resumes it.
func TestAnInterruptionThatCannotResumeSaysSo(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	_, err := f.send("Use the shared client", domain.AgentLiveSendInterrupt)
	require.NoError(t, err)
	// Another attempt of the task is still recorded as going.
	stray := uuid.New()
	f.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status)
 VALUES($1,$2,$3,$4,$5,'claude-code','autonomous','Test','["claude"]',60,'queued')`, stray, before.WorkspaceID, before.MachineID, before.ID, before.CreatorID)
	f.cleanup()
	paused := f.current()
	require.Equal(t, "blocked", paused.State)
	require.Equal(t, liveResumeFailedReason, paused.Reason)
	require.NotNil(t, paused.WorkflowPausedAt, "it is paused, and resumable")
	require.Equal(t, before.Generation, paused.Generation)
	require.Equal(t, domain.AgentLiveQueued, f.liveMessages()[0].State)

	f.exec(`UPDATE dev_machine_agent_runs SET status='cancelled',completed_at=now() WHERE id=$1`, stray)
	resumed, err := f.repo.ResumeWorkflow(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", resumed.State)
	require.True(t, resumed.LiveResumed(), "the resumed attempt is still the one the message was sent for")
	claimed, err := f.repo.ClaimLiveMessages(f.ctx, *resumed, 10000)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
}

// Cancelling while the interrupted attempt is being stopped cancels the
// task: it is not resumed behind the person's back.
func TestCancellingAnInterruptedTaskCancelsIt(t *testing.T) {
	f := newPauseFixture(t)
	_, err := f.send("Use the shared client", domain.AgentLiveSendInterrupt)
	require.NoError(t, err)
	require.NoError(t, f.repo.RequestCancel(f.ctx, f.ws, f.task.ID))
	f.cleanup()
	cancelled := f.current()
	require.Equal(t, "cancelled", cancelled.State)
	require.Nil(t, cancelled.WorkflowPausedAt)
	require.Nil(t, f.claim())
	_, err = f.send("Too late", "")
	require.ErrorIs(t, err, ErrAgentLiveMessageClosed)
}

// A step that finished first wins the race: there is no turn left to
// interrupt, the interruption is refused and queues nothing. The message can
// still be sent for the next attempt.
func TestInterruptAndSendIsRefusedOnceTheStepFinished(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	// The attempt ended; the worker has not applied its outcome yet.
	f.exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE id=$1`, before.RunID)
	_, err := f.send("Use the shared client", domain.AgentLiveSendInterrupt)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	require.Empty(t, f.liveMessages())
	require.Equal(t, "running", f.current().State)
	require.Nil(t, f.current().WorkflowPausedAt)

	// The worker moves the task on to its publication.
	moved, err := f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(before))
	require.NoError(t, err)
	_, err = f.send("Use the shared client", domain.AgentLiveSendInterrupt)
	require.ErrorIs(t, err, ErrAgentTaskConflict)
	_, err = f.send("Use the shared client", domain.AgentLiveSendStart)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "Kuayle is publishing: nothing to start")
	require.Equal(t, moved.State, f.current().State)
	queued, err := f.send("Use the shared client", "")
	require.NoError(t, err)
	require.Equal(t, domain.AgentLiveQueued, queued.State)

	// A harness without a mid-turn hook is interrupted by a plain send, and
	// only while its agent works.
	f.exec(`UPDATE agent_tasks SET provider='codex' WHERE id=$1`, f.task.ID)
	_, err = f.send("Plain send", "")
	require.NoError(t, err)
	require.Equal(t, moved.State, f.current().State, "nothing to interrupt: the message waits")
}

// Codex and OpenCode cannot be handed a message while they work: sending to
// their running attempt interrupts and resumes it.
func TestAPlainSendInterruptsAHarnessWithoutAMidTurnHook(t *testing.T) {
	f := newPauseFixture(t)
	f.exec(`UPDATE agent_tasks SET provider='codex' WHERE id=$1`, f.task.ID)
	before := f.current()
	_, err := f.send("Use the shared client", "")
	require.NoError(t, err)
	require.Equal(t, "cancelling", f.current().State)
	f.cleanup()
	resumed := f.current()
	require.Equal(t, "queued", resumed.State)
	require.Equal(t, before.Generation+1, resumed.Generation)
	require.True(t, resumed.LiveResumed())
	require.True(t, resumed.ResumeNativeSession)
}

// Send and start now: with no attempt running, the message starts one that
// continues the previous session instead of waiting for the next attempt.
func TestSendAndStartNowStartsAnAttemptForTheMessage(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	_, err := f.send("Start", domain.AgentLiveSendStart)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "an attempt is running")
	require.Empty(t, f.liveMessages())

	// The attempt fails and is cleaned up.
	require.NoError(t, f.repo.Fail(f.ctx, *f.claimed, "Native coding iteration failed; stopping execution before retry"))
	f.cleanup()
	failed := f.current()
	require.Equal(t, "failed", failed.State)

	sent, err := f.send("Try the other fixture", domain.AgentLiveSendStart)
	require.NoError(t, err)
	started := f.current()
	require.Equal(t, "queued", started.State)
	require.Equal(t, liveStartedReason, started.Reason)
	require.Equal(t, before.Generation+1, started.Generation)
	require.NotEqual(t, before.RunID, started.RunID)
	require.True(t, started.LiveResumed(), "the attempt continues the previous session, when the same profile ran it")
	require.False(t, started.ResumeNativeSession, "it is not a pause being resumed")
	require.False(t, started.FailurePending)
	require.Equal(t, before.Prompt, started.Prompt, "the task's prompt stays; the message is added to it")
	require.Equal(t, before.WorkflowSequence+1, started.WorkflowSequence, "a new visit of the implementing step")
	require.Equal(t, *before.WorkflowNode, *started.WorkflowNode)
	require.Equal(t, 2, f.step(started.WorkflowSequence).Visit)
	handoff, err := f.repo.PreviousAttempt(f.ctx, started)
	require.NoError(t, err)
	require.Equal(t, before.RunID, handoff.AttemptID)
	require.False(t, handoff.OtherProfile)
	claimed, err := f.repo.ClaimLiveMessages(f.ctx, started, 10000)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, sent.ID, claimed[0].ID)
	require.NotNil(t, f.claim())
	_, err = f.send("Again", domain.AgentLiveSendStart)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the attempt is already starting")
}

// A paused task is resumed by "send and start now" on the step it was paused
// in; a task paused by a usage limit is not started.
func TestSendAndStartNowResumesAPausedTask(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	_, err := f.repo.PauseWorkflow(f.ctx, f.ws, f.task.ID, domain.WorkflowPauseNow)
	require.NoError(t, err)
	_, err = f.send("Start", domain.AgentLiveSendStart)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the stopped attempt is still being cleaned up")
	f.cleanup()
	require.Equal(t, "blocked", f.current().State)

	_, err = f.send("Skip the migration", domain.AgentLiveSendStart)
	require.NoError(t, err)
	started := f.current()
	require.Equal(t, "queued", started.State)
	require.Equal(t, liveStartedReason, started.Reason)
	require.Nil(t, started.WorkflowPausedAt)
	require.True(t, started.ResumeNativeSession)
	require.True(t, started.LiveResumed())
	require.Equal(t, before.WorkflowSequence, started.WorkflowSequence, "the same step, not a new visit")
	require.Equal(t, before.Generation+1, started.Generation)

	f.exec(`UPDATE agent_tasks SET state='blocked',usage_limit_message='Usage limit reached' WHERE id=$1`, f.task.ID)
	_, err = f.send("Start", domain.AgentLiveSendStart)
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the provider's limit still applies")
}

// Sending is the creator's alone, however it is sent: another member, an
// admin, the owner, another workspace, a guest and someone outside a private
// team are all told the task does not exist, and nothing is queued or
// interrupted.
func TestOnlyTheCreatorTalksToTheAgent(t *testing.T) {
	f := newPauseFixture(t)
	before := f.current()
	admin := addWorkflowMember(t, f.db, f.ws, domain.RoleAdmin)
	member := addWorkflowMember(t, f.db, f.ws, domain.RoleMember)
	guest := addWorkflowMember(t, f.db, f.ws, domain.RoleGuest)
	modes := []string{"", domain.AgentLiveSendInterrupt, domain.AgentLiveSendStart}
	refused := func(ctx context.Context, ws uuid.UUID, who string) {
		t.Helper()
		for _, mode := range modes {
			_, err := f.repo.SendLiveMessage(ctx, ws, f.task.ID, "Steer someone else's agent", mode)
			require.ErrorIs(t, err, ErrConversationNotFound, "%s, mode %q", who, mode)
		}
		_, err := f.repo.LiveMessages(ctx, ws, f.task.ID, 10)
		require.ErrorIs(t, err, ErrConversationNotFound, who)
	}
	for who, user := range map[string]uuid.UUID{"admin": admin, "member": member, "guest": guest, "stranger": uuid.New()} {
		refused(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: user}), f.ws, who)
	}
	elsewhere := uuid.New()
	foreign := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: elsewhere, UserID: before.CreatorID})
	refused(foreign, elsewhere, "another workspace")
	refused(foreign, f.ws, "another workspace naming this one")

	_, err := f.repo.SendLiveMessage(f.ctx, f.ws, f.task.ID, "x", "now")
	require.ErrorIs(t, err, ErrAgentLiveMessageInvalid, "an unknown mode is refused")

	// The creator loses access: demoted to a guest, or left outside a team
	// that became private.
	f.exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, before.CreatorID)
	refused(f.ctx, f.ws, "the creator as a guest")
	f.exec(`ALTER TABLE teams DISABLE TRIGGER USER`)
	f.exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, before.CreatorID)
	f.exec(`UPDATE teams SET is_private=true WHERE id=$1`, before.TeamID)
	refused(f.ctx, f.ws, "the creator outside the private team")

	require.Zero(t, mustCount(t, f, `SELECT count(*) FROM agent_live_messages WHERE task_id=$1`), "nothing was queued")
	after := f.current()
	require.Equal(t, before.State, after.State, "and nothing was interrupted or started")
	require.Equal(t, before.Generation, after.Generation)
	require.Nil(t, after.WorkflowPausedAt)

	f.exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, before.TeamID, before.CreatorID)
	_, err = f.send("Mine again", "")
	require.NoError(t, err)
}

func mustCount(t *testing.T, f *pauseFixture, query string) int {
	t.Helper()
	var count int
	require.NoError(t, f.db.Get(&count, query, f.task.ID))
	return count
}
