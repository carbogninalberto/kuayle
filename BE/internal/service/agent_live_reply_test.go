package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A message delivered through the prompt carries its number and the reply
// rule, so the agent can answer it; one the previous attempt left unanswered
// says so. That is what the retry is told.
func TestPromptMessagesAreNumberedAndAskForAReply(t *testing.T) {
	sent := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)
	three, four := int64(3), int64(4)
	prompt := withLiveMessages("Implement", []domain.AgentLiveMessage{
		{Seq: &three, Sender: domain.AgentLiveSenderUser, Body: "Keep the v1 route", CreatedAt: sent, Deliveries: 2},
		{Seq: &four, Sender: domain.AgentLiveSenderUser, Body: "Use the shared client", CreatedAt: sent, Deliveries: 1},
	})
	require.Contains(t, prompt, "Message 3 (the previous attempt received it and ended without replying; reply first):\nFrom the task's owner")
	require.Contains(t, prompt, "Message 4:\nFrom the task's owner")
	require.Contains(t, prompt, `{"to":"orchestrator","reply_to":<message number>,"body":"..."}`)
	require.Contains(t, prompt, "what you understood and what you will do differently")
	require.Contains(t, prompt, "A step that ends with a message unanswered fails")
	require.Equal(t, 1, strings.Count(prompt, "needs your reply"), "the rule is said once")
	require.Less(t, strings.Index(prompt, "Use the shared client"), strings.Index(prompt, "reply_to"), "the rule follows the messages")

	line := withLiveChannel("Implement")
	require.Contains(t, line, `"reply_to":<its seq>`)
	require.Contains(t, line, "does not let the step finish without it")
}

// An attempt that ended without replying fails with that reason, in a
// workflow (which decides what follows) and outside one, like an attempt
// without a final message.
func TestAnAttemptWithoutAReplyFailsWithThatReason(t *testing.T) {
	message := domain.UnansweredMessageReason([]int64{3})
	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "codex"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, message, store.failReason)

	// The step's own failure transition applies and records why; here the
	// implementing step is visited again, and its attempt is given the
	// message once more.
	h := newPlanHarness(t, "{{plan}}")
	h.planRun(repository.WorkflowRun{Summary: "Do A, then B.", FinalMessage: true, WorktreeChanged: unchanged()})
	h.restart()
	h.w.task.State = "running"
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "failed"}
	sequence := h.w.task.WorkflowSequence
	require.NoError(t, h.runner.AgentFailedBecause(context.Background(), h.w.task, message))
	require.Equal(t, "implement", h.current().NodeID)
	require.Equal(t, 2, h.current().Visit)
	require.Equal(t, "Implement: "+message, h.w.steps[sequence].Detail)
}

// Interrupt and send: the resumed attempt continues the interrupted session
// and is told a person interrupted it for a message, not that it was paused
// or failed.
func TestAnInterruptedAttemptResumesItsSessionForTheMessage(t *testing.T) {
	machine := uuid.New()
	generation := int64(2)
	task := domain.AgentTask{ID: uuid.New(), Provider: "claude-code", MachineID: &machine, Generation: 2, ResumeNativeSession: true, LiveResumeGeneration: &generation}
	session := uuid.NewString()
	interrupted := &domain.AgentHandoff{AttemptID: uuid.New(), MachineID: machine, NativeSessionID: session, Status: "cancelled", OtherProfile: true}
	require.Equal(t, session, taskSession(task, interrupted).ResumeID, "the step's own session, whoever ran the step before it")
	prompt := withLiveHandoff("Add dark mode", interrupted, true, task.ResumeNativeSession, task.LiveResumed())
	require.Contains(t, prompt, "this continues your previous session")
	require.Contains(t, prompt, "interrupted the previous attempt to tell you something")
	require.Contains(t, prompt, "the worktree is unchanged")
	require.Contains(t, prompt, "Do not start over")
	require.NotContains(t, prompt, "was paused by")

	// An ordinary resume of a pause is still told it was paused.
	task.LiveResumeGeneration = nil
	require.Contains(t, withLiveHandoff("Add dark mode", interrupted, true, true, task.LiveResumed()), "paused by a person")
}

// Send and start now: the attempt continues the session of the previous one,
// however it ended, but only when the same profile ran it.
func TestAnAttemptStartedForAMessageContinuesTheSessionOfTheSameProfileOnly(t *testing.T) {
	machine := uuid.New()
	native := "0199a213-81c0-7800-8aa1-bbab2a035a53"
	generation := int64(4)
	task := domain.AgentTask{ID: uuid.New(), Provider: "codex", MachineID: &machine, Generation: 4, LiveResumeGeneration: &generation}
	for _, status := range []string{"failed", "succeeded", "timeout", "cancelled"} {
		previous := &domain.AgentHandoff{AttemptID: uuid.New(), Status: status, MachineID: machine, NativeSessionID: native}
		require.Equal(t, native, taskSession(task, previous).ResumeID, status)
	}
	failed := &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed", MachineID: machine, NativeSessionID: native}
	prompt := withLiveHandoff("Fix it", failed, true, false, true)
	require.Contains(t, prompt, "started this attempt to tell you something")
	require.Contains(t, prompt, "building on the work already in this worktree")

	other := &domain.AgentHandoff{Status: "failed", MachineID: machine, NativeSessionID: native, OtherProfile: true}
	require.Empty(t, taskSession(task, other).ResumeID, "another profile's conversation is not continued")
	require.Empty(t, taskSession(task, &domain.AgentHandoff{Status: "failed", MachineID: uuid.New(), NativeSessionID: native}).ResumeID, "the session lives on its machine")

	// Without the message the same failed attempt is not resumed.
	ordinary := task
	ordinary.LiveResumeGeneration = nil
	require.Empty(t, taskSession(ordinary, failed).ResumeID)
	later := task
	later.Generation = 5
	require.Empty(t, taskSession(later, failed).ResumeID, "only the attempt started for the message")
}
