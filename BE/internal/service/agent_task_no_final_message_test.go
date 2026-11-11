package service

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Production (KUA-136): OpenCode runs stopped on a tool call or a provider
// error and exited 0. The machine now records them as failed with this
// reason, and the task gives it instead of "Native coding iteration failed".
func TestAgentTaskFailsSayingTheAgentWroteNoFinalMessage(t *testing.T) {
	message := domain.NoFinalMessageReason("The harness reported an error: APIError: 429 overloaded")
	require.Equal(t, "The agent ended without a final message, so there is no summary of what it did. The harness reported an error: APIError: 429 overloaded", message)
	require.True(t, domain.IsNoFinalMessageReason(domain.NoFinalMessageReason("")))
	require.False(t, domain.IsNoFinalMessageReason("Native coding iteration failed; stopping execution before retry"))

	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "opencode"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, message, store.failReason)
}

// In a workflow the failure follows the step's "agent failed" transition as
// any other, the step records why, and a workflow that ends there says it.
func TestWorkflowStepWithoutAFinalMessageFailsWithThatReason(t *testing.T) {
	problem := domain.NoFinalMessageReason("")

	// Implement retries its own failure: the next visit starts, once.
	h := newPlanHarness(t, "{{plan}}")
	h.planRun(repository.WorkflowRun{Summary: "Do A, then B.", FinalMessage: true, WorktreeChanged: unchanged()})
	h.restart()
	h.w.task.State = "running"
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "failed"}
	sequence := h.w.task.WorkflowSequence
	require.NoError(t, h.runner.AgentFailedBecause(context.Background(), h.w.task, problem))
	require.Equal(t, "implement", h.current().NodeID)
	require.Equal(t, 2, h.current().Visit)
	require.Equal(t, "Implement: "+problem, h.w.steps[sequence].Detail)

	// The visit limit stops it: three visits, then the workflow fails.
	h.restart()
	h.w.task.State = "running"
	require.NoError(t, h.runner.AgentFailedBecause(context.Background(), h.w.task, problem))
	require.Equal(t, 3, h.current().Visit)
	h.restart()
	h.w.task.State = "running"
	require.NoError(t, h.runner.AgentFailedBecause(context.Background(), h.w.task, problem))
	require.Equal(t, "cancelling", h.w.task.State, "a step that never concludes does not run forever")
	require.Contains(t, h.w.task.Reason, "limit of 3 visits")

	// A step whose failure leads to an end gives the reason as the task's.
	h = newPlanHarness(t, "{{plan}}")
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "failed"}
	require.NoError(t, h.runner.AgentFailedBecause(context.Background(), h.w.task, problem))
	require.Equal(t, "failed", h.current().NodeID)
	require.Equal(t, "Workflow · Plan → Failed: agent failed. Plan: "+problem, h.w.task.Reason)
}

// Agents reported "not committed or pushed: this container has no git
// identity and no GitHub token" as a problem. Publication commits and pushes
// by design, and an implementing attempt is told so; a planning step, which
// must not change anything, is not.
func TestImplementPromptSaysKuayleCommitsAndPushes(t *testing.T) {
	prompt := withContract("Implement dark mode", domain.AgentTask{}, nil, false)
	require.Contains(t, prompt, "Kuayle commits and pushes for you")
	require.Contains(t, prompt, "no git identity and no GitHub token on purpose")
	require.Contains(t, prompt, "do not report their absence as a problem")
	require.Contains(t, prompt, "a run that ends without one fails")
	require.Equal(t, prompt, withPublication(prompt), "added once")

	require.NotContains(t, withContract("Plan dark mode", domain.AgentTask{}, nil, true), publicationLine)
}
