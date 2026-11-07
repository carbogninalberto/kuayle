package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type delegationWorkflows map[uuid.UUID][]repository.WorkflowDetail

func (f delegationWorkflows) Workflows(_ context.Context, ws uuid.UUID) ([]repository.WorkflowDetail, error) {
	return f[ws], nil
}

// A delegation can name the workflow the task follows; the approval records
// the workflow resolved in the request's workspace, so the card shows it and
// the approved task runs it.
func TestDelegationResolvesTheChosenWorkflow(t *testing.T) {
	ws, other, user := uuid.New(), uuid.New(), uuid.New()
	loop, archived, foreign := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	issue := &domain.Issue{ID: uuid.New(), Identifier: "ENG-1", Title: "Dark mode"}
	tools := NewAssistantTools(prIssues{issue: issue}, nil, prMembers{user: domain.RoleMember}, nil)
	tools.SetWorkflows(delegationWorkflows{
		ws: {
			{Workflow: domain.Workflow{ID: loop, WorkspaceID: ws, Name: "Implement with review loop"}},
			{Workflow: domain.Workflow{ID: archived, WorkspaceID: ws, Name: "Old flow", ArchivedAt: &now}},
		},
		other: {{Workflow: domain.Workflow{ID: foreign, WorkspaceID: other, Name: "Foreign"}}},
	})
	prepare := func(workflow string) (domain.DelegationPayload, error) {
		_, payload, err := tools.prepareDelegation(context.Background(), ws, user, `{"identifier":"eng-1","instructions":"Implement it","workflow":"`+workflow+`"}`)
		return payload, err
	}

	for _, chosen := range []string{"implement with review loop", loop.String()} {
		payload, err := prepare(chosen)
		require.NoError(t, err, chosen)
		require.Equal(t, loop.String(), payload.WorkflowID, "by name or ID")
		require.Equal(t, "Implement with review loop", payload.Workflow)
	}
	payload, err := prepare("standard")
	require.NoError(t, err)
	require.Equal(t, domain.StandardWorkflowID, payload.WorkflowID)
	payload, err = prepare("")
	require.NoError(t, err)
	require.Empty(t, payload.WorkflowID, "no choice keeps the issue's default")

	for _, refused := range []string{"Old flow", archived.String(), "Foreign", foreign.String(), "missing"} {
		_, err = prepare(refused)
		require.Error(t, err, "%s is not a workflow of this workspace that tasks can choose", refused)
	}

	unwired := NewAssistantTools(prIssues{issue: issue}, nil, prMembers{user: domain.RoleMember}, nil)
	_, _, err = unwired.prepareDelegation(context.Background(), ws, user, `{"identifier":"ENG-1","instructions":"x","workflow":"Implement with review loop"}`)
	require.Error(t, err, "without workflows only the default can run")
}

// The approved delegation passes the recorded workflow to the task request;
// the task service resolves and checks it again when the task is created.
func TestDelegationRequestCarriesTheWorkflow(t *testing.T) {
	options := &AgentTaskOptions{Providers: []dto.AgentProviderResponse{{ID: "codex"}}}
	request, err := delegationRequest(options, uuid.New(), "Implement it", "", "", nil, "")
	require.NoError(t, err)
	require.Nil(t, request.WorkflowID)
	request, err = delegationRequest(options, uuid.New(), "Implement it", "", "", nil, "0b2b5a4e-4a9e-4b52-9f39-7fb0f0a0a0a0")
	require.NoError(t, err)
	require.NotNil(t, request.WorkflowID)
	require.Equal(t, "0b2b5a4e-4a9e-4b52-9f39-7fb0f0a0a0a0", *request.WorkflowID)
	require.Equal(t, "codex", request.Provider)
}
