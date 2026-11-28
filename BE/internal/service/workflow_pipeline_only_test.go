package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Workflows are pipelines only: a graph cannot be created, saved, copied or
// started, the built-in Standard flow is no longer editable, and the pipeline
// templates (drafted from the old graph templates) keep working.
func TestWorkflowsArePipelinesOnly(t *testing.T) {
	f := newProfileFixture(t)
	f.tasks.SetPipelines(true)
	profile := f.storedProfile(t, "Sound", "codex", "gpt-5.5", "high")

	_, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Graph", Graph: oneStepGraph(t, profile.ID)})
	require.ErrorIs(t, err, ErrGraphWorkflowsRemoved)
	for _, template := range []string{"review_loop", "full_cycle", domain.StandardWorkflowID} {
		_, err = f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "From " + template, Template: template})
		require.ErrorIs(t, err, ErrGraphWorkflowsRemoved, template)
	}
	_, err = f.workflows.Save(f.ctx, f.ws, domain.StandardWorkflowID, WorkflowRequest{Graph: oneStepGraph(t, profile.ID)})
	require.ErrorIs(t, err, ErrGraphWorkflowsRemoved, "Standard is the built-in flow, not an editable graph")
	_, err = f.workflows.Duplicate(f.ctx, f.ws, domain.StandardWorkflowID, "")
	require.ErrorIs(t, err, ErrGraphWorkflowsRemoved)

	for _, template := range []string{"team", "standard_pipeline", "review_loop_pipeline", "full_cycle_pipeline"} {
		created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Pipeline " + template, Template: template})
		require.NoError(t, err, template)
		require.Equal(t, domain.WorkflowKindPipeline, domain.WorkflowKind(created.Graph), template)
	}

	// A graph an earlier release stored can neither be saved again nor start a
	// task, should one survive the upgrade.
	legacy, err := f.repo.CreateWorkflow(f.ctx, f.ws, f.user, "Legacy graph", "", oneStepGraph(t, profile.ID), "created")
	require.NoError(t, err)
	_, err = f.workflows.Save(f.ctx, f.ws, legacy.ID.String(), WorkflowRequest{Name: "Legacy graph", ExpectedVersion: 1, Graph: oneStepGraph(t, profile.ID)})
	require.ErrorIs(t, err, ErrGraphWorkflowsRemoved)
	legacyID := legacy.ID.String()
	request := domain.AgentTaskRequest{Provider: "codex", WorkflowID: &legacyID}
	require.ErrorIs(t, f.tasks.resolveIssueWorkflow(f.ctx, f.ws, f.issue, &request), ErrGraphWorkflowsRemoved)
	require.Nil(t, request.Workflow)

	// The built-in flow is listed, read-only.
	list, err := f.workflows.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.Equal(t, domain.StandardWorkflowID, list.Workflows[0].ID)
	require.False(t, list.Workflows[0].Customized)
}
