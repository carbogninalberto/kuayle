package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
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
