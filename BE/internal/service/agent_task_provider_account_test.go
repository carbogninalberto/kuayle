package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// creditStoreFake records the pause an account without credit asks for.
type creditStoreFake struct {
	*taskStoreFake
	paused  bool
	message string
	reason  string
}

func (f *creditStoreFake) PauseForProviderCredit(_ context.Context, _ domain.AgentTask, message, reason string) error {
	f.paused, f.message, f.reason = true, message, reason
	f.task.State = "cancelling"
	return nil
}

// selfLoopHarness is the production shape of KUA-211's workflow: the
// implementing agent's "agent failed" edge goes back to the same agent.
func selfLoopHarness(t *testing.T) *workflowHarness {
	t.Helper()
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	var graph domain.WorkflowGraph
	require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
	for i, edge := range graph.Edges {
		if edge.ID == "implement-failed" {
			graph.Edges[i].Target = "implement"
		}
	}
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	h.w.version.Graph = raw
	h.w.task.Provider = "opencode"
	h.restart()
	return h
}

// failedRun is the run the manager recorded for a failed attempt.
func failedRun(message, rule, cause string) *domain.DevMachineAgentRun {
	class := string(domain.FailureProvider)
	return &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message, FailureClass: &class, FailureRule: &rule, FailureCause: &cause}
}

// workflowPosition is what a workflow counts: its step, visits, iterations
// and the moves it applied.
type workflowPosition struct {
	sequence, iterations, entries int
	visits                        string
}

func positionOf(h *workflowHarness) workflowPosition {
	return workflowPosition{sequence: h.w.task.WorkflowSequence, iterations: h.w.task.WorkflowIterations, entries: len(h.w.entries), visits: string(h.w.task.WorkflowVisits)}
}

// Production (KUA-211): OpenCode's provider answered "Insufficient balance"
// six times and each failure took the agent_failed self-loop, using a visit
// and an iteration. Now the task pauses: the workflow is not consulted.
func TestAnAccountWithoutCreditPausesTheTaskInsteadOfFailingTheStep(t *testing.T) {
	const cause = "The provider account has no credit left: APIError: Insufficient balance (exit code 1). Standard error ended with:\ntimestamp=2026-11-17T13:30:15.525Z level=ERROR run=ses_01 message=failed error=\"APIError: Insufficient balance. Top up at https://opencode.ai/console/wrk_01JTEST0WORKSPACE/billing?credits=1\""
	run := failedRun(domain.ProviderCreditErrorMessage("APIError: Insufficient balance."), domain.FailureRuleProviderCredit, cause)

	h := selfLoopHarness(t)
	before := positionOf(h)
	store := &creditStoreFake{taskStoreFake: &taskStoreFake{task: h.w.task}}
	svc := &AgentTaskService{store: store, runtime: &taskRuntimeFake{run: run}, workflow: h.runner}
	require.NoError(t, svc.step(context.Background(), store.task))

	require.True(t, store.paused, "the task pauses")
	require.Empty(t, store.failReason, "a pause is not a failure")
	require.Equal(t, "APIError: Insufficient balance.", store.message)
	require.Contains(t, store.reason, "OpenCode's provider account has no credit left (APIError: Insufficient balance)")
	require.Contains(t, store.reason, "Top up the provider account, then resume the task")
	require.NotContains(t, store.reason, "http")
	require.NotContains(t, store.reason, "wrk_")
	require.Equal(t, before, positionOf(h), "no agent_failed edge: the visit, the iteration and the step are unchanged")
	require.Equal(t, "implement", h.current().NodeID)
	require.Nil(t, h.current().FinishedAt, "the step stays open for the resumed attempt")

	// Outside a workflow the task pauses the same way.
	store = &creditStoreFake{taskStoreFake: &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "claude-code"}}}
	run = failedRun(domain.ProviderCreditErrorMessage("Credit balance is too low"), domain.FailureRuleProviderCredit, "The provider account has no credit left: Credit balance is too low")
	require.NoError(t, (&AgentTaskService{store: store, runtime: &taskRuntimeFake{run: run}}).step(context.Background(), store.task))
	require.True(t, store.paused)
	require.Contains(t, store.reason, "Claude Code's provider account has no credit left (Credit balance is too low)")
}

// The contrast: an ordinary provider error is still the agent step's
// failure, and this workflow sends it back to the same agent.
func TestAnOrdinaryProviderErrorStillTakesTheAgentFailedEdge(t *testing.T) {
	h := selfLoopHarness(t)
	before := positionOf(h)
	store := &creditStoreFake{taskStoreFake: &taskStoreFake{task: h.w.task}}
	run := failedRun("The harness reported an error: APIError: 429 Too Many Requests", "provider_error", "The provider reported an error: The harness reported an error: APIError: 429 Too Many Requests")
	svc := &AgentTaskService{store: store, runtime: &taskRuntimeFake{run: run}, workflow: h.runner}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.False(t, store.paused)
	require.Equal(t, before.iterations+1, h.w.task.WorkflowIterations, "the self-loop used an iteration")
	require.Equal(t, 2, h.w.task.Cursor().Visits["implement"], "and a visit")
	require.Equal(t, "implement", h.current().NodeID)
}

// Production (KUA-211): agent-2's model did not exist and the agent_failed
// self-loop retried it. The task now fails at once with the model's name and
// where to change it, as a missing sign-in or a resource limit does.
func TestAModelTheProviderDoesNotOfferFailsTheTaskWithoutRetrying(t *testing.T) {
	const cause = `The provider has no model "opencode-go/kimi-k9" (exit code 1). Standard error ended with:` + "\n" + `timestamp=2026-11-17T15:02:44.120Z level=ERROR run=ses_02 message=failed ref=err_01JTEST error="ProviderModelNotFoundError: Model not found: opencode-go/kimi-k9"`
	run := failedRun(domain.ModelNotFoundErrorMessage("opencode-go/kimi-k9"), domain.FailureRuleModelNotFound, cause)

	h := selfLoopHarness(t)
	h.w.task.Model = "opencode-go/kimi-k9"
	before := positionOf(h)
	store := &creditStoreFake{taskStoreFake: &taskStoreFake{task: h.w.task}}
	svc := &AgentTaskService{store: store, runtime: &taskRuntimeFake{run: run}, workflow: h.runner}
	require.NoError(t, svc.step(context.Background(), store.task))

	require.False(t, store.paused)
	require.Equal(t, domain.FailureProvider, store.failClass)
	require.Equal(t, `Provider failure: the provider has no model "opencode-go/kimi-k9". Set a model it offers on this step's agent profile in Settings → Agents, then retry the task with that model`, store.failReason)
	require.Equal(t, before, positionOf(h), "the agent_failed self-loop is not taken")

	for name, tc := range map[string]struct {
		task   domain.AgentTask
		model  string
		reason string
	}{
		"an assigned agent": {
			domain.AgentTask{State: "running", Provider: "codex", Model: "gpt-9-codex", AgentProfileID: ptr(uuid.New())}, "gpt-9-codex",
			`Provider failure: the provider has no model "gpt-9-codex". Set a model it offers on the agent's profile in Settings → Agents, then retry the task with that model`,
		},
		"a review": {
			domain.AgentTask{State: "running", Provider: "codex", Model: "gpt-9-codex", Kind: domain.AgentTaskKindReview, WorkflowVersionID: ptr(uuid.New())}, "gpt-9-codex",
			`Provider failure: the provider has no model "gpt-9-codex". Set a model it offers on the reviewer's agent profile in Settings → Agents, or request the review with another model`,
		},
		"a task started with a model": {
			domain.AgentTask{State: "running", Provider: "claude-code", Model: "claude-sonnet-9"}, "claude-sonnet-9",
			`Provider failure: the provider has no model "claude-sonnet-9". Retry the task with a model it offers`,
		},
		"the output named no model": {
			domain.AgentTask{State: "running", Provider: "codex", Model: "gpt-9-codex"}, "",
			`Provider failure: the provider has no model "gpt-9-codex". Retry the task with a model it offers`,
		},
		"neither the output nor the task names one": {
			domain.AgentTask{State: "running", Provider: "opencode"}, "",
			`Provider failure: the provider does not offer the model the agent chose from its own configuration. Retry the task with a model it offers`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := &creditStoreFake{taskStoreFake: &taskStoreFake{task: tc.task}}
			run := failedRun(domain.ModelNotFoundErrorMessage(tc.model), domain.FailureRuleModelNotFound, "The provider has no model")
			require.NoError(t, (&AgentTaskService{store: store, runtime: &taskRuntimeFake{run: run}}).step(context.Background(), store.task))
			require.Equal(t, domain.FailureProvider, store.failClass)
			require.Equal(t, tc.reason, store.failReason)
		})
	}
}

// A reviewer step runs as its own review task. When that task failed because
// the provider does not offer the reviewer's model, the workflow fails with
// that reason instead of following the reviewer's agent_failed self-loop,
// which would start the same review with the same model again.
func TestAReviewerWhoseModelIsNotOfferedFailsTheWorkflow(t *testing.T) {
	reviewerLoop := func(t *testing.T) *workflowHarness {
		h := newWorkflowHarness(t, domain.WorkflowBudget{})
		var graph domain.WorkflowGraph
		require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
		for i, edge := range graph.Edges {
			if edge.ID == "review-failed" {
				graph.Edges[i].Target = "review"
			}
		}
		raw, err := json.Marshal(graph)
		require.NoError(t, err)
		h.w.version.Graph = raw
		h.restart()
		h.agentFinishes("Added a theme toggle")
		h.published()
		require.Equal(t, "review", h.current().NodeID)
		return h
	}
	reviewEnds := func(h *workflowHarness, review repository.WorkflowReview) {
		h.w.reviews[*h.current().ReviewTaskID] = &review
		now := h.w.now
		h.w.task.WorkflowWakeAt = &now
		require.True(t, h.tick())
	}

	h := reviewerLoop(t)
	before := positionOf(h)
	rule := domain.FailureRuleModelNotFound
	reason := `Provider failure: the provider has no model "gpt-9-codex". Set a model it offers on the reviewer's agent profile in Settings → Agents, or request the review with another model`
	reviewEnds(h, repository.WorkflowReview{State: "failed", Reason: reason, FailureRule: &rule})
	require.Equal(t, before.sequence, h.w.task.WorkflowSequence, "no agent_failed edge: the reviewer is not started again")
	require.Equal(t, before.visits, string(h.w.task.WorkflowVisits))
	require.Equal(t, "cancelling", h.w.task.State, "the task fails through cleanup")
	require.Equal(t, domain.WorkflowStatusFailed, *h.w.task.WorkflowStatus)
	require.NotNil(t, h.w.task.FailureClass)
	require.Equal(t, string(domain.FailureProvider), *h.w.task.FailureClass)
	require.Contains(t, h.w.task.Reason, `the provider has no model "gpt-9-codex"`)
	require.Contains(t, h.w.task.Reason, "Settings → Agents")

	// The contrast: a review that failed otherwise follows the edge.
	h = reviewerLoop(t)
	before = positionOf(h)
	reviewEnds(h, repository.WorkflowReview{State: "failed", Reason: "Agent failure: the review stopped"})
	require.Equal(t, "review", h.current().NodeID)
	require.Greater(t, h.w.task.WorkflowSequence, before.sequence, "the self-loop starts the reviewer again")
	require.Equal(t, 2, h.w.task.Cursor().Visits["review"])
}

// The resumed attempt is told why it was paused, so it continues the work.
func TestAttemptResumedAfterATopUpIsToldToContinue(t *testing.T) {
	prompt := withHandoff("Implement KUA-211", &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed", FailureRule: domain.FailureRuleProviderCredit}, true, true)
	require.Contains(t, prompt, "paused because the provider account ran out of credit")
	require.Contains(t, prompt, "do not start over")
	require.NotContains(t, prompt, "usage limit")
}
