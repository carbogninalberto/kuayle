package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// orchestratedGraph is the workflow behind KUA-100, KUA-171 and KUA-185: an
// orchestrator plans, an agent step implements it, the change is published,
// reviewed and checked by CI, a person approves shipping, and a last agent
// step runs before the task is done.
func orchestratedGraph(planner, implementer, reviewer uuid.UUID) WorkflowGraph {
	return WorkflowGraph{
		Nodes: []WorkflowNode{
			{ID: "start", Type: WorkflowNodeStart, Label: "Start"},
			{ID: "implement", Type: WorkflowNodeAgent, Label: "Orchestrator", ProfileID: &planner},
			{ID: "agent-1", Type: WorkflowNodeAgent, Label: "Agent step", ProfileID: &implementer},
			{ID: "publish", Type: WorkflowNodePublish, Label: "Publish"},
			{ID: "review", Type: WorkflowNodeAgent, Label: "Review", ProfileID: &reviewer, MaxVisits: 20},
			{ID: "ci", Type: WorkflowNodeCheck, Label: "CI", Check: WorkflowCheckCI, TimeoutMinutes: 120},
			{ID: "ship", Type: WorkflowNodeApproval, Label: "Ready to ship"},
			{ID: "agent-2", Type: WorkflowNodeAgent, Label: "Agent step", ProfileID: &implementer, MaxVisits: 3},
			{ID: "done", Type: WorkflowNodeEnd, Label: "Done", Outcome: WorkflowEndDone},
			{ID: "failed", Type: WorkflowNodeEnd, Label: "Failed", Outcome: WorkflowEndFailed},
		},
		Edges: []WorkflowEdge{
			{ID: "e1", Source: "start", Target: "implement", Condition: WorkflowElse},
			{ID: "e2", Source: "implement", Target: "agent-1", Condition: WorkflowAgentFinished},
			{ID: "e3", Source: "implement", Target: "failed", Condition: WorkflowAgentFailed},
			{ID: "e4", Source: "agent-1", Target: "publish", Condition: WorkflowAgentFinished},
			{ID: "e5", Source: "agent-1", Target: "failed", Condition: WorkflowAgentFailed},
			{ID: "e6", Source: "publish", Target: "review", Condition: WorkflowPublished},
			{ID: "e7", Source: "publish", Target: "failed", Condition: WorkflowPublishFailed},
			{ID: "e8", Source: "review", Target: "ci", Condition: WorkflowReviewApproved},
			{ID: "e9", Source: "review", Target: "implement", Condition: WorkflowReviewChangesRequested},
			{ID: "e10", Source: "review", Target: "failed", Condition: WorkflowAgentFailed},
			{ID: "e11", Source: "ci", Target: "ship", Condition: WorkflowChecksPassed},
			{ID: "e12", Source: "ci", Target: "implement", Condition: WorkflowChecksFailed},
			{ID: "e13", Source: "ci", Target: "failed", Condition: WorkflowTimeout},
			{ID: "e14", Source: "ship", Target: "agent-2", Condition: WorkflowApprovalApproved},
			{ID: "e15", Source: "ship", Target: "failed", Condition: WorkflowApprovalDenied},
			{ID: "e16", Source: "agent-2", Target: "done", Condition: WorkflowAgentFinished},
			{ID: "e17", Source: "agent-2", Target: "failed", Condition: WorkflowAgentFailed},
		},
		Budget: WorkflowBudget{MaxIterations: 8, OnExhausted: WorkflowOnLimitFail},
	}
}

func orchestratedProfiles() (planner, implementer, reviewer uuid.UUID, profiles map[uuid.UUID]AgentProfile) {
	implementer, reviewer, profiles = exampleProfiles()
	planner = uuid.New()
	profiles[planner] = AgentProfile{ID: planner, Name: "Orchestrator", Behaviour: AgentBehaviourPlans, Harness: "claude-code"}
	return planner, implementer, reviewer, profiles
}

// KUA-215: an iteration is one round of work. The orchestrator's plan and the
// agent step it hands off to are one iteration, so a budget of 8 allows 8
// rounds of review, not 4.
func TestAPlanAndTheStepItHandsOffToAreOneIteration(t *testing.T) {
	planner, implementer, reviewer, profiles := orchestratedProfiles()
	g := orchestratedGraph(planner, implementer, reviewer)
	require.Empty(t, ValidateWorkflowGraph(g, profiles))
	now := time.Now()
	// The task starts on the orchestrator: delegation's iteration.
	cursor := &WorkflowCursor{Node: "start", Visits: map[string]int{}, StartedAt: now}
	walk(t, g, profiles, cursor, now, WorkflowElse)
	require.Equal(t, 1, cursor.Iterations)
	for round := 1; round < 8; round++ {
		d := walk(t, g, profiles, cursor, now, WorkflowAgentFinished)
		require.Equal(t, "agent-1", d.To.ID)
		require.False(t, g.StartsIteration(d, profiles), "the implementer continues the orchestrator's round")
		walk(t, g, profiles, cursor, now, WorkflowAgentFinished)
		walk(t, g, profiles, cursor, now, WorkflowPublished)
		d = walk(t, g, profiles, cursor, now, WorkflowReviewChangesRequested)
		require.Empty(t, d.Limit, "round %d", round)
		require.Equal(t, round+1, cursor.Iterations, "one iteration per round of review")
	}
	// The eighth round still hands its plan on: a round started is finished
	// within the budget.
	d := walk(t, g, profiles, cursor, now, WorkflowAgentFinished)
	require.Empty(t, d.Limit)
	walk(t, g, profiles, cursor, now, WorkflowAgentFinished)
	walk(t, g, profiles, cursor, now, WorkflowPublished)
	d = g.Decide(*cursor, WorkflowReviewChangesRequested, now, profiles)
	require.Equal(t, WorkflowLimitIterations, d.Limit, "a ninth round would exceed 8")
	require.Equal(t, "implement", d.To.ID)

	// After the ship approval the last agent step is a round of its own.
	ship := WorkflowCursor{Node: "ship", Visits: map[string]int{}, Iterations: 8}
	require.Equal(t, WorkflowLimitIterations, g.Decide(ship, WorkflowApprovalApproved, now, profiles).Limit)
	ship.Iterations = 7
	require.Empty(t, g.Decide(ship, WorkflowApprovalApproved, now, profiles).Limit)
}

// An agent step entered after an agent step failed starts a new iteration:
// a retry is new work. A finished implementer handing on to another is not.
// The templates, whose loops all pass through a check, a review or a person,
// count as before.
func TestARetryAfterAFailedAgentIsANewIteration(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	g := FullCycleWorkflowGraph(implementer, reviewer)
	implement, _ := g.Node("implement")
	require.True(t, g.StartsIteration(WorkflowDecision{From: implement, Outcome: WorkflowAgentFailed, To: implement}, profiles))
	require.False(t, g.StartsIteration(WorkflowDecision{From: implement, Outcome: WorkflowAgentFinished, To: implement}, profiles))
	review, _ := g.Node("review")
	require.True(t, g.StartsIteration(WorkflowDecision{From: review, Outcome: WorkflowReviewChangesRequested, To: implement}, profiles))
	require.False(t, g.StartsIteration(WorkflowDecision{From: implement, Outcome: WorkflowAgentFinished, To: review}, profiles), "a review is no iteration")
	ship, _ := g.Node("ship")
	require.True(t, g.StartsIteration(WorkflowDecision{From: ship, Outcome: WorkflowBranchConflict, To: implement}, profiles), "a send-back is a new round")
}

// KUA-216: an approval reached after CI passed whose approved path runs
// through agent steps to done is the ship approval; a plan's approval, which
// also leads to agents, is not.
func TestAnApprovalBeforeAgentStepsThatEndDoneIsTheShipApproval(t *testing.T) {
	planner, implementer, reviewer, profiles := orchestratedProfiles()
	g := orchestratedGraph(planner, implementer, reviewer)
	ship, _ := g.Node("ship")
	require.Equal(t, ShipMergesAfterAgents, g.ShipApproval(ship, profiles))

	// Through two agent steps, the second planning, still.
	chained := orchestratedGraph(planner, implementer, reviewer)
	chained.Nodes = append(chained.Nodes, WorkflowNode{ID: "agent-3", Type: WorkflowNodeAgent, Label: "Notes", ProfileID: &planner})
	for i := range chained.Edges {
		if chained.Edges[i].ID == "e16" {
			chained.Edges[i].Target = "agent-3"
		}
	}
	chained.Edges = append(chained.Edges, WorkflowEdge{ID: "e18", Source: "agent-3", Target: "done", Condition: WorkflowElse})
	require.Equal(t, ShipMergesAfterAgents, chained.ShipApproval(ship, profiles))

	// Not reached after CI passed: a go-ahead for more work, not to ship.
	noCI := orchestratedGraph(planner, implementer, reviewer)
	for i := range noCI.Edges {
		switch noCI.Edges[i].ID {
		case "e11":
			noCI.Edges[i].Target = "done"
		case "e8":
			noCI.Edges[i].Target = "ship"
		}
	}
	require.Empty(t, noCI.ShipApproval(ship, profiles))

	// The agents lead to a publication, a reviewer or a failed end: not one.
	for _, target := range []string{"publish", "review", "failed"} {
		other := orchestratedGraph(planner, implementer, reviewer)
		for i := range other.Edges {
			if other.Edges[i].ID == "e16" {
				other.Edges[i].Target = target
			}
		}
		require.Empty(t, other.ShipApproval(ship, profiles), target)
	}
	// A loop of agent steps that never reaches an end is not one either.
	looping := orchestratedGraph(planner, implementer, reviewer)
	for i := range looping.Edges {
		if looping.Edges[i].ID == "e16" {
			looping.Edges[i].Target = "agent-2"
		}
	}
	require.Empty(t, looping.ShipApproval(ship, profiles))
}
