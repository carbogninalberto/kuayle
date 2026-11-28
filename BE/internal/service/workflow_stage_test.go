package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestStageAssistantCannotChooseANativeApproval(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	var graph domain.WorkflowGraph
	require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
	ship, _ := graph.Node("ship")
	ship.ProfileID = graph.FirstRun().ProfileID
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == ship.ID {
			graph.Nodes[i] = ship
		}
	}
	h.w.version.Graph = *rawJSON(t, graph)
	h.restart()
	h.w.task.WorkflowNode = &ship.ID
	h.w.steps[h.w.task.WorkflowSequence].NodeID = ship.ID
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{FinalMessage: true, Summary: "KUAYLE_STAGE_ROUTE ship-approved", WorktreeChanged: ptr(false)}
	require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "waiting for approval")
	require.Contains(t, h.w.task.Reason, "ship-approved")
	require.Contains(t, h.w.task.Reason, "Selectable transitions: none")
}

func TestTerminalAssistantHandsOffTheApprovedMerge(t *testing.T) {
	h, merger, github := toAfterAgentsShip(t)
	var graph domain.WorkflowGraph
	require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == "done" {
			implementer, ok := graph.Node("agent-2")
			require.True(t, ok)
			graph.Nodes[i].ProfileID = implementer.ProfileID
		}
	}
	h.w.version.Graph = *rawJSON(t, graph)
	h.restart()
	h.runner.SetPullRequests(github, merger)
	approved := h.head()
	require.NoError(t, h.decideAs(WorkflowDecisionApprove))
	h.agentFinishes("Updated the changelog")
	require.Equal(t, "done", h.current().NodeID)
	require.Equal(t, "queued", h.w.task.State)
	require.NotNil(t, h.current().MergeWait(), "the approved merge stays with the terminal assistant")
	require.Equal(t, approved, h.current().MergeWait().Approved)
	h.w.task.State = "running"
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "succeeded", Summary: "Ready to ship", FinalMessage: true, WorktreeChanged: ptr(false)}
	require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
	require.Equal(t, "awaiting_review", h.w.task.State)
	require.Empty(t, merger.merged)
	h.pulls.checks = passingChecks
	require.True(t, h.tick())
	require.Equal(t, []string{"acme/app#12@" + approved + "/squash"}, merger.merged)
	require.Equal(t, "completed", h.w.task.State)
}

func TestCheckStageAssistantContinuesOnPublishedWork(t *testing.T) {
	head, tree := strings.Repeat("a", 40), strings.Repeat("b", 40)
	ignored, output := strings.Repeat("c", 40), strings.Repeat("d", 40)
	before := head + " " + tree + " " + ignored
	build := head + " " + tree + " " + output
	edited := head + " " + output + " " + ignored
	commit := output + " " + tree + " " + ignored
	for _, tc := range []struct {
		name, firstAfter, after string
		files                   []string
		wantProblem             string
	}{
		{"ignored build output", "", build, []string{"files that git ignores"}, ""},
		{"retry leaves ignored output", edited, build, []string{"src/check.go"}, ""},
		{"tracked file", "", edited, []string{"src/check.go"}, "src/check.go"},
		{"tracked file named like the ignored marker", "", edited, []string{"files that git ignores"}, "files that git ignores"},
		{"legacy run with a changed file", "", "", []string{"src/check.go"}, "src/check.go"},
		{"earlier run edited a file", edited, edited, nil, "src/check.go"},
		{"new commit with identical files", "", commit, nil, "HEAD"},
		{"missing retry fingerprint", build, "", nil, "could not verify"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, input := supervisedCI(t)
			start := before
			if tc.firstAfter != "" {
				h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{WorktreeBefore: before, WorktreeAfter: tc.firstAfter, WorktreeChanged: ptr(true), WorktreeFiles: []string{"src/check.go"}}
				h.w.task.RunID = uuid.New() // A retry keeps the visit's first run and input attempt.
				start = tc.firstAfter
			}
			h.w.task.State = "running"
			h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{
				Status: "succeeded", Summary: "Ran the build.\nKUAYLE_STAGE_CONTINUE", FinalMessage: true,
				WorktreeBefore: start, WorktreeAfter: tc.after, WorktreeChanged: ptr(start != tc.after), WorktreeFiles: tc.files,
			}
			require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
			if tc.wantProblem != "" {
				require.Equal(t, "cancelling", h.w.task.State)
				require.Contains(t, h.w.task.Reason, "no transition follows")
				require.Contains(t, h.w.task.Reason, tc.wantProblem)
				return
			}
			require.Equal(t, "awaiting_review", h.w.task.State, h.w.task.Reason)
			require.Equal(t, input, h.w.task.RunID, "built-in CI uses the validated, published attempt")
			h.ci("success", "Checks passed")
			require.Equal(t, "ship", h.current().NodeID)
		})
	}
}

func supervisedCI(t *testing.T) (*workflowHarness, uuid.UUID) {
	t.Helper()
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	var graph domain.WorkflowGraph
	require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == "ci" {
			graph.Nodes[i].ProfileID = graph.FirstRun().ProfileID
		}
	}
	h.w.version.Graph = *rawJSON(t, graph)
	h.restart()
	input := h.w.task.RunID
	h.toCI("Implemented the change")
	require.Equal(t, "queued", h.w.task.State)
	require.Equal(t, input, *h.current().StageInputRunID)
	return h, input
}

func TestCheckStageAssistantRouting(t *testing.T) {
	for _, directive := range []string{"KUAYLE_STAGE_ROUTE ci-passed", "KUAYLE_STAGE_ROUTE missing", "KUAYLE_STAGE_SKIP", "KUAYLE_STAGE_ROUTE"} {
		t.Run(directive, func(t *testing.T) {
			h, input := supervisedCI(t)
			h.w.task.State = "running"
			h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{FinalMessage: true, Summary: "Reviewed CI.\n" + directive, WorktreeChanged: ptr(false)}
			require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
			require.Equal(t, "awaiting_review", h.w.task.State)
			require.Equal(t, input, h.w.task.RunID)
			require.Contains(t, h.w.task.Reason, "waiting for CI")
			require.Contains(t, h.w.task.Reason, directive)
			require.Contains(t, h.w.task.Reason, "Selectable transitions: ci-failed")
			h.ci("success", "Checks passed")
			require.Equal(t, "ship", h.current().NodeID)
		})
	}
	t.Run("built-in route with modified worktree", func(t *testing.T) {
		h, _ := supervisedCI(t)
		h.w.task.State = "running"
		h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{FinalMessage: true, Summary: "KUAYLE_STAGE_ROUTE ci-passed", WorktreeChanged: ptr(true), WorktreeFiles: []string{"main.go"}}
		require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
		require.Equal(t, "cancelling", h.w.task.State)
		require.Contains(t, h.w.task.Reason, "main.go")
		require.Contains(t, h.w.task.Reason, "ci-passed")
		require.Contains(t, h.w.task.Reason, "Selectable transitions: ci-failed")
	})
	t.Run("many selectable routes with modified worktree", func(t *testing.T) {
		h, _ := supervisedCI(t)
		var graph domain.WorkflowGraph
		require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
		for i := range 40 {
			graph.Edges = append(graph.Edges, domain.WorkflowEdge{ID: fmt.Sprintf("recover-%02d-%s", i, strings.Repeat("a", 48)), Source: "ci", Target: "implement", Condition: domain.WorkflowAssistant})
		}
		h.w.version.Graph = *rawJSON(t, graph)
		h.restart()
		h.w.task.State = "running"
		h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{FinalMessage: true, Summary: "KUAYLE_STAGE_ROUTE ci-passed", WorktreeChanged: ptr(true), WorktreeFiles: []string{"main.go"}}
		require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
		require.LessOrEqual(t, len(h.w.task.Reason), 2048, "the transition must fit the task timeline")
		require.Contains(t, h.current().Detail, graph.Edges[len(graph.Edges)-1].ID)
	})
	t.Run("selectable recovery route", func(t *testing.T) {
		h, _ := supervisedCI(t)
		h.w.task.State = "running"
		h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{FinalMessage: true, Summary: "KUAYLE_STAGE_ROUTE ci-failed", WorktreeChanged: ptr(true)}
		require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
		require.Equal(t, "queued", h.w.task.State)
		require.Equal(t, "ci-failed", *h.current().EdgeID)
	})
}
