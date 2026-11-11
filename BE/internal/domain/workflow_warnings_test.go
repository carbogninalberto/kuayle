package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPlanningStepToldToImplementIsAWarning(t *testing.T) {
	planner, builder := uuid.New(), uuid.New()
	profiles := map[uuid.UUID]AgentProfile{
		planner: {ID: planner, Name: "Orchestrator", Behaviour: AgentBehaviourPlans},
		builder: {ID: builder, Name: "Builder", Behaviour: AgentBehaviourChangesCode},
	}
	graph := func(label, prompt string, profile uuid.UUID) WorkflowGraph {
		return WorkflowGraph{Nodes: []WorkflowNode{
			{ID: "start", Type: WorkflowNodeStart, Label: "Start"},
			{ID: "step", Type: WorkflowNodeAgent, Label: label, Prompt: prompt, ProfileID: &profile},
		}}
	}
	// KUA-122: a planning profile on a step named and told to implement.
	warnings := WorkflowWarnings(graph("Implementer", "List the changes needed, then implement them. Run the relevant tests.", planner), profiles)
	require.Len(t, warnings, 2)
	require.Equal(t, "planning_step_implements", warnings[0].Code)
	require.Equal(t, "step", warnings[0].NodeID)
	require.Contains(t, warnings[0].Message, "Orchestrator")
	require.Contains(t, warnings[0].Message, "read-only")
	require.Contains(t, warnings[1].Message, "implement them")
	// The structure is still valid: a warning never refuses the graph.
	for _, issue := range ValidateWorkflowGraph(graph("Implementer", "Implement it.", planner), profiles) {
		require.NotEqual(t, "planning_step_implements", issue.Code)
	}

	for _, prompt := range []string{
		"Plan how the next agent should implement this.",
		"Do not implement anything; write the plan.",
		"Describe what to build and which tests to run.",
		"{{plan}}",
		"",
	} {
		require.Empty(t, WorkflowWarnings(graph("Planner", prompt, planner), profiles), prompt)
	}
	for _, prompt := range []string{"Implement the plan.", "Read the issue.\nThen write the code and commit.", "First fix the failing checks", "Please open a pull request"} {
		require.Len(t, WorkflowWarnings(graph("Planner", prompt, planner), profiles), 1, prompt)
	}
	// The same words are fine for a profile that changes code.
	require.Empty(t, WorkflowWarnings(graph("Implementer", "Implement the plan.", builder), profiles))
}
