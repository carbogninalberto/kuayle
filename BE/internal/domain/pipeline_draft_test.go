package domain

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The built-in templates are single paths once their ways back are set
// aside, so each drafts a pipeline that saves as it is.
func TestDraftPipelineFromTheTemplates(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	writer := PipelineMember{Role: "implementer", ProfileID: implementer, Writer: true}
	verdict := PipelineCheck{ID: "reviewer", Fact: PipelineFactVerdict, Role: "reviewer", Required: true}
	pr := PipelineCheck{ID: "pr-exists", Fact: PipelineFactPRExists, Required: true}
	ci := PipelineCheck{ID: "ci", Fact: PipelineFactCIGreen, Required: true}
	approved := PipelineCheck{ID: "owner", Fact: PipelineFactApproved, Required: true}

	// Standard as the editor's copy has it: the implementer on its step and
	// a limit on the human review that sends the work back.
	standard := StandardWorkflowGraph()
	for i := range standard.Nodes {
		switch standard.Nodes[i].ID {
		case "implement":
			standard.Nodes[i].ProfileID = &implementer
		case "review":
			standard.Nodes[i].MaxVisits = 6
		}
	}
	cases := []struct {
		name  string
		graph WorkflowGraph
		want  Pipeline
	}{
		{"standard", standard, Pipeline{
			Kind: WorkflowKindPipeline,
			Phases: []PipelinePhase{
				{ID: "implement", Label: "Implement", Owner: "implementer", Prompt: "The task's instructions"},
				{ID: "publish", Label: "Publish pull request", Owner: PipelineOwnerKuayle, Checks: []PipelineCheck{pr}},
				// Approving the review merges: the merge becomes the guarded phase.
				{ID: "review", Label: "Human review", Owner: PipelineOwnerHuman, Checks: []PipelineCheck{approved}},
				{ID: "merge", Label: "Merge", Owner: PipelineOwnerKuayle, Guarded: true},
			},
			Roster: []PipelineMember{writer},
		}},
		{"review loop", ReviewLoopWorkflowGraph(implementer, reviewer), Pipeline{
			Kind: WorkflowKindPipeline,
			Phases: []PipelinePhase{
				// The room carries the review comments and failing checks the
				// graph's variables named.
				{ID: "implement", Label: "Implementer", Owner: "implementer", Prompt: "Address the feedback below, then make sure the change is complete."},
				{ID: "publish", Label: "Open pull request", Owner: PipelineOwnerKuayle, Checks: []PipelineCheck{pr}},
				{ID: "review", Label: "Reviewer", Owner: "implementer", Checks: []PipelineCheck{verdict}},
				{ID: "ci", Label: "CI", Owner: "implementer", Checks: []PipelineCheck{ci}},
				{ID: "ship", Label: "Ready to ship", Owner: PipelineOwnerHuman, Checks: []PipelineCheck{approved}},
				{ID: "merge", Label: "Merge", Owner: PipelineOwnerKuayle, Guarded: true},
			},
			Roster: []PipelineMember{writer, {Role: "reviewer", ProfileID: reviewer}},
			// Eight iterations of two roles.
			Budget: PipelineBudget{MaxTurns: 16, MaxMinutes: 24 * 60, OnExhausted: WorkflowOnLimitFail},
		}},
		{"full cycle", FullCycleWorkflowGraph(implementer, reviewer), Pipeline{
			Kind: WorkflowKindPipeline,
			Phases: []PipelinePhase{
				{ID: "implement", Label: "Implement", Owner: "implementer", Prompt: "Address what sent the work back, then make sure the change is complete."},
				{ID: "publish", Label: "Open pull request", Owner: PipelineOwnerKuayle, Checks: []PipelineCheck{pr}},
				{ID: "review", Label: "Review", Owner: "implementer", Checks: []PipelineCheck{verdict}},
				{ID: "ci", Label: "CI", Owner: "implementer", Checks: []PipelineCheck{ci}},
				{ID: "ship", Label: "Ready to ship", Owner: PipelineOwnerHuman, Checks: []PipelineCheck{approved}},
				{ID: "merge", Label: "Merge", Owner: PipelineOwnerKuayle, Guarded: true},
			},
			Roster: []PipelineMember{writer, {Role: "reviewer", ProfileID: reviewer}},
			Budget: PipelineBudget{MaxTurns: 20, MaxMinutes: 2 * 24 * 60, OnExhausted: WorkflowOnLimitAsk},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := clone(t, tc.graph)
			draft, reasons := DraftPipeline(tc.graph, profiles)
			require.Empty(t, reasons)
			require.Equal(t, tc.want, *draft)
			require.Empty(t, ValidatePipeline(*draft, profiles))
			require.Equal(t, before, tc.graph, "the graph is read, never changed")
		})
	}
}

// Steps that have a pipeline equivalent, or none to have, draft too: a
// second step for the same profile that changes code is another phase of
// the writer, and a retired local check, which does nothing, has no phase.
func TestDraftPipelineKeepsTheWriterAndDropsRetiredChecks(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	g := ReviewLoopWorkflowGraph(implementer, reviewer)
	g = insertAfterImplement(g, WorkflowNode{ID: "polish", Type: WorkflowNodeAgent, Label: "Polish", ProfileID: &implementer, Prompt: "Tidy up.\n\n{{diff}}"}, WorkflowAgentFinished)
	g = insertAfterImplement(g, WorkflowNode{ID: "tests", Type: WorkflowNodeCheck, Label: "Local tests", Check: WorkflowCheckTests, MaxVisits: 3}, WorkflowChecksPassed)
	g.Edges = append(g.Edges, WorkflowEdge{ID: "tests-failed", Source: "tests", Target: "implement", Condition: WorkflowChecksFailed})
	require.Empty(t, ValidateStoredWorkflowGraph(g, profiles))

	draft, reasons := DraftPipeline(g, profiles)
	require.Empty(t, reasons)
	require.Equal(t, []string{"implement", "polish", "publish", "review", "ci", "ship", "merge"}, phaseIDs(*draft))
	require.Equal(t, PipelinePhase{ID: "polish", Label: "Polish", Owner: "implementer", Prompt: "Tidy up."}, draft.Phases[1])
	require.Len(t, draft.Roster, 2)
}

// A graph that is not one path, or has a step no phase can stand for, has
// no draft: the reasons name the steps.
func TestDraftPipelineSaysWhyNot(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	planner, polisher := uuid.New(), uuid.New()
	profiles[planner] = AgentProfile{ID: planner, Name: "Planner", Behaviour: AgentBehaviourPlans, Harness: "codex"}
	profiles[polisher] = AgentProfile{ID: polisher, Name: "Polisher", Behaviour: AgentBehaviourChangesCode, Harness: "codex"}
	edge := func(g *WorkflowGraph, id string) *WorkflowEdge {
		for i := range g.Edges {
			if g.Edges[i].ID == id {
				return &g.Edges[i]
			}
		}
		t.Fatalf("no transition %s", id)
		return nil
	}
	node := func(g *WorkflowGraph, id string) *WorkflowNode {
		for i := range g.Nodes {
			if g.Nodes[i].ID == id {
				return &g.Nodes[i]
			}
		}
		t.Fatalf("no step %s", id)
		return nil
	}
	cases := []struct {
		name   string
		change func(g *WorkflowGraph)
		code   string
		node   string
	}{
		{"a branch", func(g *WorkflowGraph) {
			// Changes requested go to another step, which goes on to CI.
			g.Nodes = append(g.Nodes, WorkflowNode{ID: "fix", Type: WorkflowNodeAgent, Label: "Fix", ProfileID: &implementer})
			edge(g, "review-changes").Target = "fix"
			g.Edges = append(g.Edges, WorkflowEdge{ID: "fix-ci", Source: "fix", Target: "ci", Condition: WorkflowAgentFinished},
				WorkflowEdge{ID: "fix-failed", Source: "fix", Target: "failed", Condition: WorkflowAgentFailed})
		}, "branching", "review"},
		{"a planning step", func(g *WorkflowGraph) {
			g.Nodes = append(g.Nodes, WorkflowNode{ID: "plan", Type: WorkflowNodeAgent, Label: "Plan", ProfileID: &planner})
			edge(g, "start-implement").Target = "plan"
			g.Edges = append(g.Edges, WorkflowEdge{ID: "plan-implement", Source: "plan", Target: "implement", Condition: WorkflowAgentFinished})
		}, "planning_step", "plan"},
		{"a stage assistant", func(g *WorkflowGraph) { node(g, "ci").ProfileID = &implementer }, "stage_assistant", "ci"},
		{"a second profile that changes code", func(g *WorkflowGraph) {
			*g = insertAfterImplement(*g, WorkflowNode{ID: "polish", Type: WorkflowNodeAgent, Label: "Polish", ProfileID: &polisher}, WorkflowAgentFinished)
		}, "second_writer", "polish"},
		{"an approval before the pull request", func(g *WorkflowGraph) {
			*g = insertAfterImplement(*g, WorkflowNode{ID: "check-in", Type: WorkflowNodeApproval, Label: "Check in", MaxVisits: 3}, WorkflowApprovalApproved)
			g.Edges = append(g.Edges, WorkflowEdge{ID: "check-in-denied", Source: "check-in", Target: "implement", Condition: WorkflowApprovalDenied})
		}, "early_approval", "check-in"},
		{"code changed after the approval", func(g *WorkflowGraph) {
			// The ship approval leads to an agent that changes code, and
			// Kuayle merges at the end (ShipMergesAfterAgents).
			g.Nodes = append(g.Nodes, WorkflowNode{ID: "changelog", Type: WorkflowNodeAgent, Label: "Changelog", ProfileID: &implementer})
			edge(g, "ship-approved").Target = "changelog"
			g.Edges = append(g.Edges, WorkflowEdge{ID: "changelog-done", Source: "changelog", Target: "done", Condition: WorkflowAgentFinished})
		}, "code_after_approval", "changelog"},
		{"no way on", func(g *WorkflowGraph) {
			// Only publishing nothing reaches the done end, which a pipeline
			// leaves out like the graph's default.
			g.Edges = append(g.Edges, WorkflowEdge{ID: "publish-nothing", Source: "publish", Target: "done", Condition: WorkflowNoChanges})
			edge(g, "ship-approved").Target = "failed"
		}, "dead_end", "ship"},
		{"a merge nobody approved", func(g *WorkflowGraph) {
			g.Nodes = append(g.Nodes, WorkflowNode{ID: "merge", Type: WorkflowNodeMerge, Label: "Merge"})
			edge(g, "ci-passed").Target = "merge"
			g.Edges = append(g.Edges, WorkflowEdge{ID: "merge-done", Source: "merge", Target: "done", Condition: WorkflowMerged})
			g.Nodes = slices.DeleteFunc(g.Nodes, func(n WorkflowNode) bool { return n.ID == "ship" })
			g.Edges = slices.DeleteFunc(g.Edges, func(e WorkflowEdge) bool { return e.Source == "ship" })
		}, "missing_approval", "merge"},
		{"a problem of the graph", func(g *WorkflowGraph) { node(g, "review").ProfileID = ptrTo(uuid.New()) }, "missing_profile", "review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := ReviewLoopWorkflowGraph(implementer, reviewer)
			tc.change(&g)
			draft, reasons := DraftPipeline(g, profiles)
			require.Nil(t, draft)
			require.NotEmpty(t, reasons)
			require.Equal(t, tc.code, reasons[0].Code, reasons)
			require.Equal(t, tc.node, reasons[0].NodeID, reasons)
			require.NotEmpty(t, reasons[0].Message)
		})
	}
}

// insertAfterImplement puts a step between the implementer and the step its
// finished transition led to; the step leaves on condition.
func insertAfterImplement(g WorkflowGraph, step WorkflowNode, condition string) WorkflowGraph {
	for i := range g.Edges {
		if g.Edges[i].Source == "implement" && g.Edges[i].Condition == WorkflowAgentFinished {
			next := g.Edges[i].Target
			g.Edges[i].Target = step.ID
			g.Edges = append(g.Edges, WorkflowEdge{ID: step.ID + "-on", Source: step.ID, Target: next, Condition: condition})
			break
		}
	}
	g.Nodes = append(g.Nodes, step)
	return g
}

func phaseIDs(p Pipeline) []string {
	ids := []string{}
	for _, phase := range p.Phases {
		ids = append(ids, phase.ID)
	}
	return ids
}

func ptrTo[T any](v T) *T { return &v }

func clone(t *testing.T, g WorkflowGraph) WorkflowGraph {
	t.Helper()
	out := g
	out.Nodes = slices.Clone(g.Nodes)
	out.Edges = slices.Clone(g.Edges)
	return out
}
