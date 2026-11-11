package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The world applies a person's pause and resume through the same
// computations the repository uses (ComputeWorkflowPause and
// ComputeWorkflowResume), as it does for moves.

func (s worldStore) ParkWorkflow(_ context.Context, task domain.AgentTask) error {
	if task.Generation != s.w.task.Generation || s.w.task.WorkflowPauseRequest == nil || s.w.task.WorkflowPausedAt != nil {
		return repository.ErrAgentTaskConflict
	}
	now := s.w.now
	s.w.task.WorkflowPauseRequest, s.w.task.WorkflowPausedAt = nil, &now
	return nil
}

// pause is the creator's pause request; it returns what it did.
func (h *workflowHarness) pause(mode string) (string, error) {
	h.t.Helper()
	run := ""
	if recorded := h.w.runs[h.w.task.RunID]; recorded != nil {
		run = recorded.Status
	} else if h.w.task.State == "running" {
		run = "running"
	}
	effect, err := repository.ComputeWorkflowPause(h.w.task, mode, run)
	if err != nil {
		return "", err
	}
	now := h.w.now
	switch effect {
	case repository.WorkflowPauseRequested:
		h.w.task.WorkflowPauseRequest = &mode
	case repository.WorkflowPauseInterrupts:
		// The attempt is cancelled and its runtime cleaned up, which ends in
		// blocked (FinishCancellation).
		h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "cancelled"}
		h.w.task.WorkflowPauseRequest, h.w.task.WorkflowPausedAt, h.w.task.State = nil, &now, "blocked"
	default:
		h.w.task.WorkflowPauseRequest, h.w.task.WorkflowPausedAt = nil, &now
	}
	return effect, nil
}

func (h *workflowHarness) resume() error {
	h.t.Helper()
	resume, err := repository.ComputeWorkflowResume(h.w.task, h.w.now)
	if err != nil {
		return err
	}
	h.w.task.WorkflowPauseRequest, h.w.task.WorkflowPausedAt = nil, nil
	if resume.Withdrawn {
		return nil
	}
	h.w.task.WorkflowBudgetFrom = resume.BudgetFrom
	step := h.w.steps[h.w.task.WorkflowSequence]
	step.PausedSeconds += int(resume.Paused / time.Second)
	if resume.Restart {
		h.w.task.Generation++
		h.w.task.RunID = uuid.New()
		h.w.task.State, h.w.task.ResumeNativeSession = "queued", true
		generation := h.w.task.Generation
		step.Generation = &generation
	}
	if h.w.task.State == "awaiting_review" {
		now := h.w.now
		h.w.task.WorkflowWakeAt = &now
	}
	return nil
}

// finish drives the example workflow from its implementing step to the end.
func (h *workflowHarness) finish() {
	h.t.Helper()
	h.agentFinishes("Added a theme toggle")
	h.published()
	h.review("approve", "Looks good")
	h.ci("success", "")
	h.decide(WorkflowDecisionApprove)
}

func TestWorkflowPausedDuringAnAgentStepFinishesLikeAnUninterruptedRun(t *testing.T) {
	reference := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 5})
	reference.finish()

	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 5})
	visits, iterations, sequence := string(h.w.task.WorkflowVisits), h.w.task.WorkflowIterations, h.w.task.WorkflowSequence
	effect, err := h.pause(domain.WorkflowPauseNow)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowPauseInterrupts, effect, "pause now stops the running agent")
	require.Equal(t, "blocked", h.w.task.State)
	require.NotNil(t, h.w.task.WorkflowPausedAt)
	require.Equal(t, domain.WorkflowStatusRunning, *h.w.task.WorkflowStatus, "the workflow is neither failed nor at a limit")

	// The interrupted attempt reports nothing to the workflow while paused.
	require.ErrorIs(t, h.runner.Outcome(context.Background(), h.w.task, domain.WorkflowAgentFailed, "The agent's iteration failed"), repository.ErrAgentTaskConflict)
	require.Equal(t, "implement", h.current().NodeID)
	require.Nil(t, h.current().FinishedAt)

	h.w.now = h.w.now.Add(6 * time.Hour)
	h.restart()
	require.NoError(t, h.resume())
	require.Equal(t, "queued", h.w.task.State, "the step continues as a new attempt")
	require.True(t, h.w.task.ResumeNativeSession, "which continues the paused attempt's native session")
	require.EqualValues(t, 2, h.w.task.Generation)
	require.Equal(t, sequence, h.w.task.WorkflowSequence, "on the same step")
	require.JSONEq(t, visits, string(h.w.task.WorkflowVisits), "resuming is not another visit")
	require.Equal(t, iterations, h.w.task.WorkflowIterations, "nor another iteration of the budget")
	require.EqualValues(t, 2, *h.current().Generation)
	require.Equal(t, "Add dark mode", h.w.task.Prompt, "with the step's own prompt")

	h.finish()
	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, reference.path(), h.path())
	require.Equal(t, reference.w.task.Cursor().Visits, h.w.task.Cursor().Visits)
	require.Equal(t, reference.w.task.WorkflowIterations, h.w.task.WorkflowIterations)
	require.Equal(t, *reference.w.task.WorkflowStatus, *h.w.task.WorkflowStatus)
}

func TestWorkflowPausedWhileWaitingForCIAdvancesOnlyAfterResume(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	h.agentFinishes("Added a theme toggle")
	h.published()
	h.review("approve", "Looks good")
	require.Equal(t, "ci", h.current().NodeID)

	for _, mode := range []string{domain.WorkflowPauseNow, domain.WorkflowPauseAfterStep} {
		effect, err := h.pause(mode)
		require.NoError(t, err)
		require.Equal(t, repository.WorkflowPauseTakesEffect, effect, "a waiting step has nothing to stop")
		require.Equal(t, "awaiting_review", h.w.task.State)

		// CI finishes on GitHub and its webhook wakes the step: nothing moves.
		h.github(PullRequestCheck{Name: "Tests", State: "passed"})
		h.signalCI("success", "")
		require.False(t, h.tick(), "the worker leaves a paused task alone")
		require.Equal(t, "ci", h.current().NodeID)
		require.Nil(t, h.current().FinishedAt)
		if mode == domain.WorkflowPauseNow {
			// Resumed and paused again before the worker looked.
			require.NoError(t, h.resume())
		}
	}
	require.NoError(t, h.resume())
	require.True(t, h.tick(), "resuming evaluates the step at once")
	require.Equal(t, "ship", h.current().NodeID)

	// An approval is a waiting step too: nobody decides it while paused.
	_, err := h.pause(domain.WorkflowPauseNow)
	require.NoError(t, err)
	_, err = h.runner.Decide(context.Background(), h.w.task.WorkspaceID, h.w.task.ID, h.w.task.WorkflowSequence, WorkflowDecisionApprove)
	require.ErrorIs(t, err, ErrWorkflowDecision)
	require.Equal(t, "ship", h.current().NodeID)
	require.NoError(t, h.resume())
	h.decide(WorkflowDecisionApprove)
	require.Equal(t, "completed", h.w.task.State)
}

func TestWorkflowPausedTimeCountsAgainstNoBudgetOrTimeout(t *testing.T) {
	// Without a pause the hour is used up while CI runs.
	control := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60})
	control.agentFinishes("Added a theme toggle")
	control.published()
	control.review("approve", "Looks good")
	control.w.now = control.w.now.Add(3 * time.Hour)
	now := control.w.now
	control.w.task.WorkflowWakeAt = &now
	require.True(t, control.tick())
	require.Equal(t, domain.WorkflowStatusFailed, *control.w.task.WorkflowStatus)

	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxMinutes: 60})
	h.agentFinishes("Added a theme toggle")
	h.published()
	h.review("approve", "Looks good")
	h.w.now = h.w.now.Add(20 * time.Minute)
	_, err := h.pause(domain.WorkflowPauseNow)
	require.NoError(t, err)
	// Longer than the time budget (an hour) and the CI step's timeout (two).
	h.w.now = h.w.now.Add(3 * time.Hour)
	require.NoError(t, h.resume())
	require.Equal(t, 3*60*60, h.current().PausedSeconds)
	require.True(t, h.tick())
	require.Equal(t, "ci", h.current().NodeID, "the step still waits")
	require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)

	// The 40 minutes left of the budget still run out.
	h.w.now = h.w.now.Add(39 * time.Minute)
	now = h.w.now
	h.w.task.WorkflowWakeAt = &now
	require.True(t, h.tick())
	require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)
	h.w.now = h.w.now.Add(time.Minute)
	now = h.w.now
	h.w.task.WorkflowWakeAt = &now
	require.True(t, h.tick())
	require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)
	require.Contains(t, h.w.task.Reason, "time budget of 60 minutes")

	// A step's own timeout does not count the pause either.
	h = newWorkflowHarness(t, domain.WorkflowBudget{})
	h.agentFinishes("Added a theme toggle")
	h.published()
	h.review("approve", "Looks good")
	_, err = h.pause(domain.WorkflowPauseNow)
	require.NoError(t, err)
	h.w.now = h.w.now.Add(5 * time.Hour)
	require.NoError(t, h.resume())
	require.True(t, h.tick())
	require.Equal(t, "ci", h.current().NodeID)
	h.w.now = h.w.now.Add(121 * time.Minute)
	now = h.w.now
	h.w.task.WorkflowWakeAt = &now
	require.True(t, h.tick())
	require.Equal(t, "failed", h.current().NodeID, "the CI step times out after its own two hours")
}

func TestWorkflowPauseAfterThisStep(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	effect, err := h.pause(domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowPauseRequested, effect)
	require.Equal(t, "running", h.w.task.State, "the agent keeps working")
	require.Nil(t, h.w.task.WorkflowPausedAt)

	h.agentFinishes("Added a theme toggle")
	require.NotNil(t, h.w.task.WorkflowPausedAt, "the pause takes effect when the step has an outcome")
	require.Nil(t, h.w.task.WorkflowPauseRequest)
	require.Equal(t, "implement", h.current().NodeID)
	require.Equal(t, "running", h.w.task.State)
	require.Empty(t, h.w.entries, "no step starts while paused")

	require.NoError(t, h.resume())
	require.Equal(t, "running", h.w.task.State, "nothing is run again")
	require.EqualValues(t, 1, h.w.task.Generation)
	// The worker claims the task again and reports the same finished run.
	require.NoError(t, h.runner.AgentFinished(context.Background(), h.w.task))
	require.Equal(t, "publishing", h.w.task.State)
	require.Equal(t, []string{"start", "implement", "publish"}, h.path())

	// A publication is Kuayle's own work: "pause now" lets it finish first.
	effect, err = h.pause(domain.WorkflowPauseNow)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowPauseRequested, effect)
	h.published()
	require.NotNil(t, h.w.task.WorkflowPausedAt)
	require.Equal(t, "publish", h.current().NodeID)
	require.NoError(t, h.resume())
	h.published()
	require.Equal(t, "review", h.current().NodeID)
}

func TestWorkflowPauseRequestsThatConflict(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	_, err := h.pause("later")
	require.ErrorIs(t, err, repository.ErrConversationInvalid)
	require.ErrorIs(t, h.resume(), repository.ErrAgentTaskConflict, "only a paused workflow resumes")

	// A request can be withdrawn, or turned into a pause now.
	_, err = h.pause(domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	_, err = h.pause(domain.WorkflowPauseAfterStep)
	require.ErrorIs(t, err, repository.ErrAgentTaskConflict)
	require.NoError(t, h.resume())
	require.Nil(t, h.w.task.WorkflowPauseRequest)
	_, err = h.pause(domain.WorkflowPauseAfterStep)
	require.NoError(t, err)
	effect, err := h.pause(domain.WorkflowPauseNow)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowPauseInterrupts, effect)
	_, err = h.pause(domain.WorkflowPauseNow)
	require.ErrorIs(t, err, repository.ErrAgentTaskConflict, "a paused workflow is not paused again")

	// Tasks that do not run a workflow, or no longer run, are not paused.
	standard := newWorkflowHarness(t, domain.WorkflowBudget{})
	standard.w.task.WorkflowVersionID = nil
	_, err = standard.pause(domain.WorkflowPauseNow)
	require.ErrorIs(t, err, repository.ErrAgentTaskConflict)
	for _, state := range []string{"completed", "failed", "cancelled", "cancelling", "blocked"} {
		ended := newWorkflowHarness(t, domain.WorkflowBudget{})
		ended.w.task.State = state
		_, err = ended.pause(domain.WorkflowPauseNow)
		require.ErrorIs(t, err, repository.ErrAgentTaskConflict, state)
	}
}

// A resumed attempt continues the native session of the attempt a person
// paused, and is told so; without a session to resume it gets the handoff.
func TestResumedAttemptContinuesThePausedSession(t *testing.T) {
	machine := uuid.New()
	task := domain.AgentTask{ID: uuid.New(), Provider: "claude-code", MachineID: &machine, Generation: 2, ResumeNativeSession: true}
	session := uuid.NewString()
	paused := &domain.AgentHandoff{AttemptID: uuid.New(), MachineID: machine, NativeSessionID: session, Status: "cancelled"}
	require.Equal(t, session, taskSession(task, paused).ResumeID)
	prompt := withHandoff("Add dark mode", paused, true, task.ResumeNativeSession)
	require.Contains(t, prompt, "this continues your previous session")
	require.Contains(t, prompt, "paused by a person")
	require.NotContains(t, prompt, "usage limit")

	// A harness that recorded no session, or a machine that was replaced.
	for _, previous := range []*domain.AgentHandoff{
		{AttemptID: uuid.New(), MachineID: machine, Status: "cancelled"},
		{AttemptID: uuid.New(), MachineID: uuid.New(), NativeSessionID: session, Status: "cancelled"},
	} {
		fresh := taskSession(task, previous)
		require.Empty(t, fresh.ResumeID)
		require.True(t, agent.ValidSessionID(fresh.NewID))
		prompt = withHandoff("Add dark mode", previous, false, task.ResumeNativeSession)
		require.Contains(t, prompt, "this is a new session")
		require.Contains(t, prompt, "paused by a person")
		require.Contains(t, prompt, "do not start over")
	}
}
