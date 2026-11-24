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
// own rules, but no task starts on it and it cannot become a default:
// nothing runs pipelines yet, and the graph engine must never run one.
func TestTeamPipelineTemplateAndTheSwitch(t *testing.T) {
	f := newProfileFixture(t)
	_, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Team", Template: "team"})
	require.ErrorIs(t, err, ErrPipelinesDisabled)

	f.tasks.SetPipelines(true)
	created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Team", Template: "team"})
	require.NoError(t, err)
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
	err = start()
	require.ErrorIs(t, err, ErrPipelineNotRunnable)
	message, permanent := delegationFailure(err)
	require.True(t, permanent)
	require.Equal(t, "Pipeline workflows cannot run tasks yet", message)
	_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: &created.ID})
	require.ErrorIs(t, err, ErrPipelineNotRunnable)
	_, err = f.workflows.Save(f.ctx, f.ws, domain.StandardWorkflowID, WorkflowRequest{Graph: created.Graph})
	var input *ErrInvalidWorkflowInput
	require.ErrorAs(t, err, &input, "every task without another workflow runs Standard")

	// Switched off again: a saved pipeline is neither started, saved nor copied.
	f.tasks.SetPipelines(false)
	err = start()
	require.ErrorIs(t, err, ErrPipelinesDisabled)
	message, permanent = delegationFailure(err)
	require.True(t, permanent)
	require.Equal(t, "Pipeline workflows are turned off on this server", message)
	_, err = f.workflows.Save(f.ctx, f.ws, created.ID, WorkflowRequest{Name: "Team", Graph: created.Graph, ExpectedVersion: 1})
	require.ErrorIs(t, err, ErrPipelinesDisabled)
	_, err = f.workflows.Duplicate(f.ctx, f.ws, created.ID, "")
	require.ErrorIs(t, err, ErrPipelinesDisabled)

	var tasks int
	require.NoError(t, f.db.Get(&tasks, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Zero(t, tasks)
}

func codesOf(issues []domain.WorkflowIssue) []string {
	codes := []string{}
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	return codes
}
