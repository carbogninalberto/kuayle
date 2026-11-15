package service

import (
	"context"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The graph edits below use the harness's editGraph helper from
// workflow_plan_test.go: it parses the pinned graph, applies the change and
// restarts the runner, whose graph cache is then empty.

// publishedNothing reports a publication step that found no changes against
// the base, as AgentTaskService.step does for a workflow task.
func (h *workflowHarness) publishedNothing(reason string) {
	h.t.Helper()
	require.Equal(h.t, "publishing", h.w.task.State)
	service := &AgentTaskService{workflow: h.runner}
	require.NoError(h.t, service.workflowPublished(context.Background(), h.w.task, &domain.DevMachineAgentRunStep{Status: domain.DevMachineAgentRunStepStatusSkipped, Summary: &reason}))
}

const noChangesReason = "The agent made no changes against main; nothing to publish. The work may already be merged, or the run only produced a summary."

// KUA-176: an iteration that changed nothing against the base is not a failed
// publication; the workflow ends the task as done, without a pull request and
// without another iteration.
func TestWorkflowPublicationWithNoChangesEndsTheTaskWithoutAPullRequest(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 16, OnExhausted: domain.WorkflowOnLimitFail})
	h.agentFinishes("Proposal: split the importer into two stages")
	h.publishedNothing(noChangesReason)

	require.Equal(t, "completed", h.w.task.State, "the task ends instead of failing")
	require.Equal(t, domain.WorkflowStatusDone, *h.w.task.WorkflowStatus)
	require.Contains(t, h.w.task.Reason, noChangesReason)
	require.NotContains(t, h.w.task.Reason, "publishing failed")

	step := h.current()
	require.Equal(t, "publish", step.NodeID, "no transition is taken")
	require.NotNil(t, step.Outcome)
	require.Equal(t, "no_changes", *step.Outcome)
	require.Equal(t, 1, h.w.task.WorkflowIterations)
	require.Equal(t, []string{"start", "implement", "publish"}, h.path())
}

// KUA-176: an "else" transition written before the outcome existed usually
// means "publishing failed, try again"; it must not send an iteration that
// changed nothing back to the agent.
func TestWorkflowNoChangesIsNotCaughtByAnElseTransition(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 16, OnExhausted: domain.WorkflowOnLimitFail})
	h.editGraph(func(g *domain.WorkflowGraph) {
		for i := range g.Edges {
			if g.Edges[i].ID == "publish-failed" {
				g.Edges[i] = domain.WorkflowEdge{ID: "publish-else", Source: "publish", Target: "implement", Condition: domain.WorkflowElse}
			}
		}
	})
	h.agentFinishes("Nothing to change")
	h.publishedNothing(noChangesReason)

	require.Equal(t, "completed", h.w.task.State)
	require.Equal(t, 1, h.w.task.WorkflowIterations, "no loop back to the implementer")
	require.Equal(t, []string{"start", "implement", "publish"}, h.path())
}

// KUA-176: a workflow that routes the outcome itself, here to a person's
// approval, is followed.
func TestWorkflowFollowsAnExplicitNoChangesTransition(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 16, OnExhausted: domain.WorkflowOnLimitFail})
	h.editGraph(func(g *domain.WorkflowGraph) {
		g.Edges = append(g.Edges, domain.WorkflowEdge{ID: "publish-nothing", Source: "publish", Target: "ship", Condition: "no_changes"})
	})
	h.agentFinishes("Proposal only")
	h.publishedNothing(noChangesReason)

	require.Equal(t, "ship", h.current().NodeID)
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.Equal(t, domain.WorkflowStatusWaiting, *h.w.task.WorkflowStatus)
	require.True(t, h.w.entries[len(h.w.entries)-1].NeedsPerson)
	require.NotNil(t, h.current().Condition)
	require.Equal(t, "no_changes", *h.current().Condition)
}
