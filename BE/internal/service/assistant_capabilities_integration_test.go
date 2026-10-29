package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func disableAssistantTool(t *testing.T, f toolFixture, name string) {
	t.Helper()
	_, err := f.db.Exec(`INSERT INTO assistant_tool_settings(workspace_id,tool_name,enabled) VALUES($1,$2,false)
		ON CONFLICT(workspace_id,tool_name) DO UPDATE SET enabled=false`, f.ws, name)
	require.NoError(t, err)
}

func offeredToolNames(request map[string]any) []string {
	names := []string{}
	tools, _ := request["tools"].([]any)
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	return names
}

// A disabled tool is not offered to the model, and a call to it anyway is
// recorded as refused without running.
func TestDisabledAssistantToolIsNotOfferedAndNeverRuns(t *testing.T) {
	f := newToolFixture(t)
	f.service.tools.SetSettings(repository.NewAssistantToolSettingsRepository(f.db))
	disableAssistantTool(t, f, "create_issue")
	a := f.attempt(t, "File a bug")
	provider, requests := scriptedProvider(t, toolCallStream("create_issue", `{"team_key":"ENG","title":"Login bug"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "File"}}, func(string) error { return nil }))

	offered := offeredToolNames((*requests)[0])
	require.Contains(t, offered, "list_teams")
	require.NotContains(t, offered, "create_issue", "a disabled tool is not offered")
	var issues int
	require.NoError(t, f.db.Get(&issues, `SELECT count(*) FROM issues WHERE workspace_id=$1`, f.ws))
	require.Zero(t, issues, "the disabled tool never ran")
	var status, failure string
	require.NoError(t, f.db.QueryRow(`SELECT status,error FROM conversation_tool_calls WHERE message_id=$1`, a.Message.ID).Scan(&status, &failure))
	require.Equal(t, "failed", status)
	require.Equal(t, "create_issue is disabled in this workspace", failure)
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "disabled in this workspace")
}

// Disabling a tool after its approval was requested makes approving it fail
// cleanly; nothing starts.
func TestPendingApprovalOfADisabledToolFailsWhenApproved(t *testing.T) {
	f := newToolFixture(t)
	settings := repository.NewAssistantToolSettingsRepository(f.db)
	f.service.tools.SetSettings(settings)
	a, approval, tasks := requestDelegation(t, f)
	f.service.approvals.SetToolSettings(settings)
	disableAssistantTool(t, f, "delegate_issue")

	resolved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", resolved.Status)
	require.NotNil(t, resolved.Error)
	require.Equal(t, "This tool is disabled in this workspace", *resolved.Error)
	require.Zero(t, tasks.creates, "a disabled tool's approval starts nothing")
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Zero(t, count)
}

// Every built-in tool is listed from the registry with its approval need,
// availability and workspace switch. Members read; owners and admins write.
func TestAssistantCapabilitiesListTheRegistryAndOnlyAdminsSwitchTools(t *testing.T) {
	f := newToolFixture(t)
	f.service.tools.SetSettings(repository.NewAssistantToolSettingsRepository(f.db))
	disableAssistantTool(t, f, "search_issues")

	listed, err := f.service.Capabilities(f.ctx, f.ws)
	require.NoError(t, err)
	require.False(t, listed.CanManage, "members read the list")
	require.Len(t, listed.Tools, len(assistantToolCatalog)+1, "every catalog tool and image filing are listed")
	byName := map[string]AssistantCapability{}
	for _, tool := range listed.Tools {
		require.NotEmpty(t, tool.Description, tool.Name)
		byName[tool.Name] = tool
	}
	for _, tool := range assistantToolCatalog {
		require.Contains(t, byName, tool.name, "new registry tools appear automatically")
	}
	require.Equal(t, AssistantCapability{Name: "list_teams", Description: byName["list_teams"].Description, Available: true, Enabled: true}, byName["list_teams"])
	require.False(t, byName["search_issues"].Enabled)
	require.True(t, byName["delegate_issue"].RequiresApproval)
	require.True(t, byName["create_issue"].ChangesData)
	require.False(t, byName["get_agent_work"].Available, "agent work is not wired in this fixture")
	require.False(t, byName[attachImagesToolName].Available, "image attachments are not wired in this fixture")

	_, err = f.service.SetCapability(f.ctx, f.ws, "search_issues", true)
	require.ErrorIs(t, err, repository.ErrAssistantToolSettingDenied, "members cannot switch tools")

	_, err = f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, err = f.service.SetCapability(f.ctx, f.ws, "no_such_tool", false)
	require.ErrorIs(t, err, ErrUnknownAssistantTool)
	updated, err := f.service.SetCapability(f.ctx, f.ws, "search_issues", true)
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	updated, err = f.service.SetCapability(f.ctx, f.ws, "delegate_issue", false)
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.True(t, updated.RequiresApproval)
	listed, err = f.service.Capabilities(f.ctx, f.ws)
	require.NoError(t, err)
	require.True(t, listed.CanManage)

	_, err = f.service.Capabilities(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: uuid.New()}), f.ws)
	require.ErrorIs(t, err, ErrAssistantCapabilitiesDenied, "outsiders cannot read the list")
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, err = f.service.Capabilities(f.ctx, f.ws)
	require.ErrorIs(t, err, ErrAssistantCapabilitiesDenied, "guests cannot read the list")
}
