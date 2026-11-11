package service

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Kuayle records what sends the work back, and what people decide, in the
// task's notebook without the agent: review findings, failing CI, a pause
// at a limit and the person's decision.
func TestWorkflowRecordsFindingsAndDecisionsInTheNotebook(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{MaxIterations: 2, OnExhausted: domain.WorkflowOnLimitAsk})
	h.agentFinishes("first")
	require.Empty(t, h.w.notes, "an agent's summary is not a finding")
	h.published()
	h.review("request_changes", "- The toggle is not keyboard accessible")
	require.Len(t, h.w.notes, 1)
	require.Equal(t, domain.NotebookFindingReview, h.w.notes[0].FindingSource)
	require.Contains(t, h.w.notes[0].Finding, "not keyboard accessible")
	require.Nil(t, h.w.notes[0].Instruction, "a reviewer's findings are never a person's instruction")

	h.agentFinishes("second")
	h.published()
	h.review("approve", "ok")
	h.ci("failure", "TestThemeToggle failed")
	var ci, paused bool
	for _, note := range h.w.notes {
		ci = ci || note.FindingSource == domain.NotebookFindingCI && note.Instruction == nil
		paused = paused || note.Event != "" && note.Finding == ""
	}
	require.True(t, ci, "the failing CI check is a finding")
	require.True(t, paused, "the pause at the iteration budget is an event")

	before := len(h.w.notes)
	h.decide(WorkflowDecisionContinue)
	require.Greater(t, len(h.w.notes), before)
	require.Contains(t, h.w.notes[len(h.w.notes)-1].Event, `A person chose "continue"`)
}
