package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The pipeline templates (KUA-290) are the pipeline versions of Standard,
// Review loop and Full cycle: each is drafted from its graph template, so a
// change to the graph template reaches its pipeline, and each passes the
// pipeline validator as it is.
func TestPipelineTemplatesAreTheGraphTemplatesDrafted(t *testing.T) {
	implementer, reviewer := uuid.New(), uuid.New()
	profiles := map[uuid.UUID]domain.AgentProfile{
		implementer: {ID: implementer, Name: "Implementer", Behaviour: domain.AgentBehaviourChangesCode, Harness: "claude-code"},
		reviewer:    {ID: reviewer, Name: "Reviewer", Behaviour: domain.AgentBehaviourReviews, Harness: "codex"},
	}
	cases := []struct {
		template string
		graph    domain.WorkflowGraph
		phases   []string
		roles    []string
		budget   domain.PipelineBudget
	}{
		// Standard: no agent review and no CI step, and approving the human
		// review merges.
		{"standard_pipeline", standardCopy(implementer), []string{"implement", "publish", "review", "merge"}, []string{"implementer"}, domain.PipelineBudget{}},
		{"review_loop_pipeline", domain.ReviewLoopWorkflowGraph(implementer, reviewer), []string{"implement", "publish", "review", "ci", "ship", "merge"},
			[]string{"implementer", "reviewer"}, domain.PipelineBudget{MaxTurns: 16, MaxMinutes: 24 * 60, OnExhausted: domain.WorkflowOnLimitFail}},
		{"full_cycle_pipeline", domain.FullCycleWorkflowGraph(implementer, reviewer), []string{"implement", "publish", "review", "ci", "ship", "merge"},
			[]string{"implementer", "reviewer"}, domain.PipelineBudget{MaxTurns: 20, MaxMinutes: 2 * 24 * 60, OnExhausted: domain.WorkflowOnLimitAsk}},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			p, err := pipelineTemplate(tc.template, implementer, reviewer, profiles)
			require.NoError(t, err)
			require.Empty(t, domain.ValidatePipeline(*p, profiles))
			phases := []string{}
			for _, phase := range p.Phases {
				phases = append(phases, phase.ID)
			}
			require.Equal(t, tc.phases, phases)
			roles := []string{}
			for _, member := range p.Roster {
				roles = append(roles, member.Role)
			}
			require.Equal(t, tc.roles, roles)
			require.Equal(t, tc.budget, p.Budget)
			require.True(t, p.Phases[len(p.Phases)-1].Guarded, "Kuayle merges, after a person approved")

			// Each phase keeps the name of the step it stands for.
			labels := map[string]string{}
			for _, node := range tc.graph.Nodes {
				labels[node.ID] = node.Label
			}
			for _, phase := range p.Phases[:len(p.Phases)-1] {
				require.Equal(t, labels[phase.ID], phase.Label, phase.ID)
			}
		})
	}

	// Only those three have a pipeline version; the team template is its own.
	for _, name := range []string{"team_pipeline", "blank_pipeline", "_pipeline", "review_loop"} {
		_, err := pipelineTemplate(name, implementer, reviewer, profiles)
		var input *ErrInvalidWorkflowInput
		require.ErrorAs(t, err, &input, name)
	}

	// A template whose profiles are gone has no draft, and says why.
	_, err := pipelineTemplate("review_loop_pipeline", implementer, reviewer, map[uuid.UUID]domain.AgentProfile{implementer: profiles[implementer]})
	var invalid *domain.WorkflowInvalidError
	require.ErrorAs(t, err, &invalid)
}

// Creating a workflow from a pipeline template stores the pipeline of the
// workspace's built-in profiles while pipelines are on, and a task starts on
// it, run by its tick; with the switch off each is refused and nothing is
// stored.
func TestCreateFromPipelineTemplates(t *testing.T) {
	f := newProfileFixture(t)
	templates := []string{"standard_pipeline", "review_loop_pipeline", "full_cycle_pipeline"}
	for _, template := range templates {
		_, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: template, Template: template})
		require.ErrorIs(t, err, ErrPipelinesDisabled, template)
	}
	var stored int
	require.NoError(t, f.db.Get(&stored, `SELECT count(*) FROM workflows WHERE workspace_id=$1`, f.ws))
	require.Zero(t, stored)

	f.tasks.SetPipelines(true)
	implementer, reviewer, err := f.workflows.builtinProfileIDs(f.ctx, f.ws)
	require.NoError(t, err)
	profiles, err := f.repo.ActiveProfiles(f.ctx, f.ws)
	require.NoError(t, err)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	for _, template := range templates {
		created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: template, Description: "From " + template, Template: template})
		require.NoError(t, err, template)
		require.Equal(t, domain.WorkflowKindPipeline, domain.WorkflowKind(created.Graph))
		got, err := domain.ParsePipeline(created.Graph)
		require.NoError(t, err)
		want, err := pipelineTemplate(template, implementer, reviewer, profiles)
		require.NoError(t, err)
		require.Equal(t, *want, *got)
		issues, err := f.workflows.Validate(f.ctx, f.ws, created.Graph)
		require.NoError(t, err)
		require.Empty(t, issues, template)

		_, err = f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement it", Provider: "codex", WorkflowID: &created.ID})
		require.NoError(t, err, template)
		var task domain.AgentTask
		require.NoError(t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE workspace_id=$1 AND workflow_version_id=$2`, f.ws, created.VersionID))
		require.True(t, task.Pipeline, template)
		require.Equal(t, "implement", *task.WorkflowNode)
		_, err = f.db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, task.ID)
		require.NoError(t, err)
	}
	_, err = f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Team", Template: "team_pipeline"})
	var input *ErrInvalidWorkflowInput
	require.ErrorAs(t, err, &input)
}
