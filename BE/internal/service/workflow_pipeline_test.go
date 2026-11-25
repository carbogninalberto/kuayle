package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The "team" template is a pipeline of the built-in profiles. With the
// server's switch off it is refused; on, it is saved and validated by its
// own rules, a task starts on it (its tick runs it, never the graph engine)
// with the writer's profile and its writer as a member, and it can become a
// default.
func TestTeamPipelineTemplateAndTheSwitch(t *testing.T) {
	f := newProfileFixture(t)
	_, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Team", Template: "team"})
	require.ErrorIs(t, err, ErrPipelinesDisabled)

	list, err := f.workflows.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.False(t, list.PipelinesEnabled, "the composer offers no pipeline")
	f.tasks.SetPipelines(true)
	created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Team", Template: "team"})
	require.NoError(t, err)
	list, err = f.workflows.List(f.ctx, f.ws)
	require.NoError(t, err)
	require.True(t, list.PipelinesEnabled)
	implementer, reviewer, err := f.workflows.builtinProfileIDs(f.ctx, f.ws)
	require.NoError(t, err)
	stored, err := domain.ParsePipeline(created.Graph)
	require.NoError(t, err)
	require.Equal(t, domain.TeamPipeline(implementer, reviewer), *stored)

	issues, err := f.workflows.Validate(f.ctx, f.ws, created.Graph)
	require.NoError(t, err)
	require.Empty(t, issues)
	broken := *stored
	broken.Roster = []domain.PipelineMember{{Role: "implementer", ProfileID: uuid.New(), Writer: true}}
	raw := mustJSON(t, broken)
	issues, err = f.workflows.Validate(f.ctx, f.ws, raw)
	require.NoError(t, err)
	require.Equal(t, []string{"missing_profile", "invalid_check"}, codesOf(issues), "the reviewer the review phase asks is gone too")

	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	start := func() error {
		_, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement it", Provider: "codex", WorkflowID: &created.ID})
		return err
	}
	require.NoError(t, start())
	var task domain.AgentTask
	require.NoError(t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.True(t, task.Pipeline)
	require.Equal(t, "implement", *task.WorkflowNode)
	require.Equal(t, domain.WorkflowStatusRunning, *task.WorkflowStatus)
	profiles, err := f.repo.ActiveProfiles(f.ctx, f.ws)
	require.NoError(t, err)
	require.Equal(t, profiles[implementer].Harness, task.Provider, "the writer's profile runs the first turn")
	members, err := f.tasks.Repo.Members(f.ctx, task.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, "implementer", members[0].Role)
	_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: &created.ID})
	require.NoError(t, err)
	_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: ptr(domain.StandardWorkflowID)})
	require.NoError(t, err)
	_, err = f.workflows.Save(f.ctx, f.ws, domain.StandardWorkflowID, WorkflowRequest{Graph: created.Graph})
	var input *ErrInvalidWorkflowInput
	require.ErrorAs(t, err, &input, "every task without another workflow runs Standard")

	// Switched off again: a saved pipeline is neither started, saved nor copied.
	f.tasks.SetPipelines(false)
	err = start()
	require.ErrorIs(t, err, ErrPipelinesDisabled)
	message, permanent := delegationFailure(err)
	require.True(t, permanent)
	require.Equal(t, "Pipeline workflows are turned off on this server", message)
	_, err = f.workflows.Save(f.ctx, f.ws, created.ID, WorkflowRequest{Name: "Team", Graph: created.Graph, ExpectedVersion: 1})
	require.ErrorIs(t, err, ErrPipelinesDisabled)
	_, err = f.workflows.Duplicate(f.ctx, f.ws, created.ID, "")
	require.ErrorIs(t, err, ErrPipelinesDisabled)

	var tasks int
	require.NoError(t, f.db.Get(&tasks, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, tasks)
}

func codesOf(issues []domain.WorkflowIssue) []string {
	codes := []string{}
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	return codes
}

// A pipeline is a scope's default like any workflow while pipelines are on
// (KUA-291): a task started without a choice runs it, on its tick. Off, it
// cannot become one, and a task that would start on the default it already
// is is refused with the reason, not run on another workflow.
func TestPipelineAsAScopesDefault(t *testing.T) {
	f := newProfileFixture(t)
	f.tasks.SetPipelines(true)
	created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Review loop", Template: "review_loop_pipeline"})
	require.NoError(t, err)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT team_id FROM issues WHERE id=$1`, f.issue))
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	start := func() (*domain.AgentTask, error) {
		return f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Implement it", Provider: "codex"})
	}

	f.tasks.SetPipelines(false)
	_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeTeam, ScopeID: team, WorkflowID: &created.ID})
	require.ErrorIs(t, err, ErrPipelinesDisabled)
	f.tasks.SetPipelines(true)
	list, err := f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeTeam, ScopeID: team, WorkflowID: &created.ID})
	require.NoError(t, err)
	require.Len(t, list.Defaults, 1)
	require.Equal(t, created.ID, list.Defaults[0].WorkflowID.String())

	task, err := start()
	require.NoError(t, err)
	require.True(t, task.Pipeline, "the team's default runs")
	require.Equal(t, *created.VersionID, *task.WorkflowVersionID)
	_, err = f.db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, task.ID)
	require.NoError(t, err)

	f.tasks.SetPipelines(false)
	_, err = start()
	require.ErrorIs(t, err, ErrPipelinesDisabled)
	var tasks int
	require.NoError(t, f.db.Get(&tasks, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, tasks)
}
