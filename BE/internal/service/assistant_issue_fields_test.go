package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type issueFieldMembers []domain.WorkspaceMemberWithUser

func (m issueFieldMembers) ListMembersWithUsers(context.Context, uuid.UUID) ([]domain.WorkspaceMemberWithUser, error) {
	return m, nil
}

func TestAssistantIssueAssignees(t *testing.T) {
	actor, casey, otherCasey := uuid.New(), uuid.New(), uuid.New()
	tools := AssistantTools{issueMembers: issueFieldMembers{
		{UserID: actor, Name: "Alex", Email: "alex@example.test"},
		{UserID: casey, Name: "Me", DisplayName: "Casey", Email: "casey@example.test"},
		{UserID: otherCasey, Name: "Casey", Email: "other-casey@example.test"},
	}}
	ids, err := tools.resolveIssueAssignees(t.Context(), uuid.New(), actor, []string{" ME ", "CASEY@example.test", "me"})
	require.NoError(t, err)
	require.Equal(t, []string{actor.String(), casey.String()}, ids)

	for _, name := range []string{"Casey", "Unknown Person"} {
		ids, err = tools.resolveIssueAssignees(t.Context(), uuid.New(), actor, []string{"me", name})
		require.Nil(t, ids)
		require.ErrorContains(t, err, name)
		require.ErrorContains(t, err, "casey@example.test")
		require.ErrorContains(t, err, "other-casey@example.test")
	}
}

func TestAssistantIssueDateValidation(t *testing.T) {
	tools := AssistantTools{}
	for _, value := range []string{"2026-11-2", "2026-02-30", "Friday", "", "0000-01-01"} {
		var req dto.UpdateIssueRequest
		err := tools.resolveIssueFields(t.Context(), uuid.Nil, uuid.Nil, uuid.Nil, nil, assistantIssueFields{DueDate: &value}, &req)
		require.ErrorContains(t, err, "YYYY-MM-DD")
		require.ErrorContains(t, err, value)
	}
}

func TestAssistantIssueOptionSuggestions(t *testing.T) {
	var options []issueFieldOption
	for i := range 6 {
		options = append(options, issueFieldOption{uuid.New(), []string{fmt.Sprintf("Maintenance %d", i)}})
	}
	options = append(options, issueFieldOption{uuid.New(), []string{"Sprint X"}})
	_, err := resolveIssueFieldOption("cycle", "Sprint Z", options)
	require.ErrorContains(t, err, `cycle value "Sprint Z" did not match`)
	require.ErrorContains(t, err, `"Sprint X"`)
	require.NotContains(t, err.Error(), "more options")

	for i := range 25 {
		options = append(options, issueFieldOption{uuid.New(), []string{fmt.Sprintf("Release %d", i)}})
	}
	_, err = resolveIssueFieldOption("cycle", "Sprint Z", options)
	require.ErrorContains(t, err, `"Release 17"`)
	require.ErrorContains(t, err, "more options")
	require.NotContains(t, err.Error(), `"Release 18"`)
}

type issueFieldWriter struct {
	issue   *domain.Issue
	updated dto.UpdateIssueRequest
}

func (w *issueFieldWriter) Create(context.Context, uuid.UUID, uuid.UUID, dto.CreateIssueRequest) (*domain.Issue, error) {
	return w.issue, nil
}

func (w *issueFieldWriter) Update(_ context.Context, _, _ uuid.UUID, _ string, req dto.UpdateIssueRequest) (*domain.Issue, error) {
	w.updated = req
	return w.issue, nil
}

func TestAssistantIssueClearsOptionalFields(t *testing.T) {
	ws, user, project := uuid.New(), uuid.New(), uuid.New()
	issue := &domain.Issue{ID: uuid.New(), TeamID: uuid.New(), ProjectID: &project, Identifier: "ENG-1"}
	repo := &mockIssueRepo{}
	repo.On("GetByIdentifier", mock.Anything, ws, issue.Identifier).Return(issue, nil)
	writer := &issueFieldWriter{issue: issue}
	tools := NewAssistantTools(repo, nil, memberFake{role: domain.RoleMember}, writer)
	_, _, err := tools.run(t.Context(), ws, user, "update_issue", `{"identifier":"ENG-1","due_date":"","cycle":"","project":""}`)
	require.NoError(t, err)
	for _, field := range []*string{writer.updated.DueDate, writer.updated.CycleID, writer.updated.ProjectID} {
		require.NotNil(t, field)
		require.Empty(t, *field)
	}
}

func TestAssistantIssueCreateReportsSavedIssueWhenFieldsUnconfirmed(t *testing.T) {
	ws, user := uuid.New(), uuid.New()
	team := domain.Team{ID: uuid.New(), Key: "ENG"}
	issue := &domain.Issue{ID: uuid.New(), Identifier: "ENG-7"}
	repo := &mockIssueRepo{}
	tools := NewAssistantTools(repo, wikiToolTeams{teams: []domain.Team{team}}, memberFake{role: domain.RoleMember}, &issueFieldWriter{issue: issue})
	tools.issueMembers = issueFieldMembers{{UserID: user}}
	tools.SetIssueLabels(&fakeLabelRepo{labels: []domain.Label{{ID: uuid.New(), WorkspaceID: ws, Name: "Needs QA"}}})
	repo.On("GetAssignees", mock.Anything, issue.ID).Return([]uuid.UUID(nil), errors.New("read failed")).Once()
	repo.On("GetLabels", mock.Anything, issue.ID).Return([]domain.Label(nil), nil).Once()

	for _, tc := range []struct{ field, input string }{
		{"assignees", `{"team_key":"ENG","title":"Ship it","assignees":["me"]}`},
		{"labels", `{"team_key":"ENG","title":"Ship it","labels":["Needs QA"]}`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			_, id, err := tools.run(t.Context(), ws, user, "create_issue", tc.input)
			require.Equal(t, &issue.ID, id)
			require.ErrorContains(t, err, "Issue ENG-7 was created")
			require.ErrorContains(t, err, "requested "+tc.field+" could not be confirmed")
			require.ErrorContains(t, err, "use update_issue")
		})
	}
	repo.AssertExpectations(t)
}

func TestAssistantIssueToolFieldContract(t *testing.T) {
	for _, name := range []string{"create_issue", "update_issue"} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok)
		var schema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal([]byte(tool.parameters), &schema))
		for field, typ := range map[string]string{"assignees": "array", "due_date": "string", "cycle": "string", "project": "string", "labels": "array"} {
			require.Equal(t, typ, schema.Properties[field].Type)
			require.NotEmpty(t, schema.Properties[field].Description)
			require.ErrorContains(t, decodeIssueWriteInput(`{"`+field+`":null}`, &assistantIssueFields{}), field+" cannot be null")
		}
	}
}
