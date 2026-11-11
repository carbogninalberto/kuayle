package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A task moves to a newer version of its workflow only onto the step it is
// on: the same step, doing the same kind of work.
func TestMatchWorkflowNodeKeepsOnlyTheSameStep(t *testing.T) {
	coder, planner, reviewer, otherCoder := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	profiles := map[uuid.UUID]AgentProfile{
		coder:      {ID: coder, Behaviour: AgentBehaviourChangesCode},
		otherCoder: {ID: otherCoder, Behaviour: AgentBehaviourChangesCode},
		planner:    {ID: planner, Behaviour: AgentBehaviourPlans},
		reviewer:   {ID: reviewer, Behaviour: AgentBehaviourReviews},
	}
	from := WorkflowGraph{Nodes: []WorkflowNode{
		{ID: "implement", Type: WorkflowNodeAgent, Label: "Implement", ProfileID: &coder},
		{ID: "ci", Type: WorkflowNodeCheck, Check: WorkflowCheckCI, Label: "CI"},
		{ID: "ship", Type: WorkflowNodeApproval, Label: "Ready to ship"},
	}}
	to := WorkflowGraph{Nodes: []WorkflowNode{
		{ID: "implement", Type: WorkflowNodeAgent, Label: "Build it", ProfileID: &otherCoder, Prompt: "new prompt", MaxVisits: 9},
		{ID: "ci", Type: WorkflowNodeCheck, Check: WorkflowCheckTests, Label: "Tests"},
		{ID: "review", Type: WorkflowNodeAgent, Label: "Review", ProfileID: &reviewer},
	}}

	// Same step with a new label, prompt, limit and another profile that
	// also changes code: the task continues on it.
	node, refused := MatchWorkflowNode("implement", from, to, profiles)
	require.Empty(t, refused)
	require.Equal(t, "Build it", node.Label)

	// The step is gone.
	_, refused = MatchWorkflowNode("ship", from, to, profiles)
	require.Equal(t, WorkflowSwitchStepRemoved, refused)

	// The id is kept but the step now does something else.
	_, refused = MatchWorkflowNode("ci", from, to, profiles)
	require.Equal(t, WorkflowSwitchStepChanged, refused)
	planned := WorkflowGraph{Nodes: []WorkflowNode{{ID: "implement", Type: WorkflowNodeAgent, ProfileID: &planner}}}
	_, refused = MatchWorkflowNode("implement", from, planned, profiles)
	require.Equal(t, WorkflowSwitchStepChanged, refused, "a step that planned instead of changing code is another step")
	retyped := WorkflowGraph{Nodes: []WorkflowNode{{ID: "implement", Type: WorkflowNodeApproval}}}
	_, refused = MatchWorkflowNode("implement", from, retyped, profiles)
	require.Equal(t, WorkflowSwitchStepChanged, refused)
	missingProfile := uuid.New()
	orphan := WorkflowGraph{Nodes: []WorkflowNode{{ID: "implement", Type: WorkflowNodeAgent, ProfileID: &missingProfile}}}
	_, refused = MatchWorkflowNode("implement", from, orphan, profiles)
	require.Equal(t, WorkflowSwitchStepChanged, refused, "a profile that no longer exists cannot be matched")

	// A task that is not on a step of its own version cannot be placed.
	_, refused = MatchWorkflowNode("nowhere", from, to, profiles)
	require.Equal(t, WorkflowSwitchStepRemoved, refused)
}

// Visits carry over for the steps the new version still has.
func TestCarriedWorkflowVisitsDropsRemovedSteps(t *testing.T) {
	to := WorkflowGraph{Nodes: []WorkflowNode{{ID: "implement"}, {ID: "review"}}}
	require.Equal(t, map[string]int{"implement": 3, "review": 1},
		CarriedWorkflowVisits(map[string]int{"implement": 3, "review": 1, "ship": 2}, to))
	require.Equal(t, map[string]int{}, CarriedWorkflowVisits(nil, to))
}

// A task is moved only while nothing is acting on its workflow: paused by a
// person, or waiting on a step.
func TestWorkflowSwitchNeedsATaskAtRest(t *testing.T) {
	version := uuid.New()
	status := func(v string) *string { return &v }
	task := AgentTask{WorkflowVersionID: &version, State: "running", WorkflowStatus: status(WorkflowStatusRunning)}
	require.Equal(t, WorkflowSwitchNotAtRest, WorkflowSwitchBlocked(task))
	task.WorkflowStatus = status(WorkflowStatusWaiting)
	task.State = "awaiting_review"
	require.Empty(t, WorkflowSwitchBlocked(task))
	task.WorkflowStatus = status(WorkflowStatusRunning)
	task.State = "blocked"
	paused := task.CreatedAt
	task.WorkflowPausedAt = &paused
	require.Empty(t, WorkflowSwitchBlocked(task))
	// A pause that is still being carried out has an attempt to stop first.
	task.State = "cancelling"
	require.Equal(t, WorkflowSwitchNotAtRest, WorkflowSwitchBlocked(task))
	for _, state := range []string{"completed", "cancelled", "failed"} {
		task.State = state
		require.Equal(t, WorkflowSwitchFinished, WorkflowSwitchBlocked(task), state)
	}
	task.State = "awaiting_review"
	task.WorkflowStatus = status(WorkflowStatusDone)
	require.Equal(t, WorkflowSwitchFinished, WorkflowSwitchBlocked(task))
	require.Equal(t, WorkflowSwitchNoWorkflow, WorkflowSwitchBlocked(AgentTask{State: "running"}))
	require.Equal(t, WorkflowSwitchNoWorkflow, WorkflowSwitchBlocked(AgentTask{WorkflowVersionID: &version, Kind: AgentTaskKindReview, State: "running"}))
}
