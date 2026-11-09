package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// wikiApprovalFixture is a tool fixture with the ENG team wiki wired into the
// assistant's tools and approvals, as in production.
func wikiApprovalFixture(t *testing.T) (toolFixture, *WikiService, domain.WikiScope) {
	t.Helper()
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	wiki := NewWikiService(repository.NewWikiRepository(f.db))
	members := repository.NewWorkspaceRepository(f.db)
	f.service.tools.SetWiki(wiki, repository.NewProjectRepository(f.db))
	approvals := NewAssistantApprovals(f.repo, &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)})
	approvals.SetWiki(wiki, members)
	f.service.SetApprovals(approvals)
	return f, wiki, domain.TeamWiki(team)
}

// requestWikiApproval drives an assistant reply that calls one destructive
// wiki tool and returns the approval it left pending.
func requestWikiApproval(t *testing.T, f toolFixture, tool, arguments string) (domain.ConversationAttempt, domain.ConversationApproval) {
	t.Helper()
	a := f.attempt(t, "Please do it")
	provider, requests := scriptedProvider(t, toolCallStream(tool, arguments), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Do it"}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	return a, h.Approvals[0]
}

func TestWikiDeleteWaitsForApproval(t *testing.T) {
	f, wiki, scope := wikiApprovalFixture(t)
	page, err := wiki.Create(f.ctx, f.ws, scope, f.user, dto.CreateWikiPageRequest{Title: "Old runbook", Content: "<p>x</p>"})
	require.NoError(t, err)

	a, approval := requestWikiApproval(t, f, wikiDeleteAction, `{"team_key":"ENG","page_id":"`+page.ID.String()+`"}`)
	var payload domain.WikiApprovalPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, "Old runbook", payload.Title)
	require.Equal(t, "ENG", payload.ScopeName)
	_, err = wiki.Get(f.ctx, f.ws, scope, page.ID)
	require.NoError(t, err, "asking deletes nothing")

	resolved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Nil(t, resolved.Error)
	_, err = wiki.Get(f.ctx, f.ws, scope, page.ID)
	require.ErrorIs(t, err, ErrWikiNotFound)
}

func TestWikiRestoreNeverOverwritesANewerPage(t *testing.T) {
	f, wiki, scope := wikiApprovalFixture(t)
	page, err := wiki.Create(f.ctx, f.ws, scope, f.user, dto.CreateWikiPageRequest{Title: "Runbook", Content: "<p>v1</p>"})
	require.NoError(t, err)
	v2 := "<p>v2</p>"
	_, err = wiki.Update(f.ctx, f.ws, scope, page.ID, f.user, dto.UpdateWikiPageRequest{Content: &v2, Version: 1})
	require.NoError(t, err)
	revisions, err := wiki.Revisions(f.ctx, f.ws, scope, page.ID)
	require.NoError(t, err)
	restore := `{"team_key":"ENG","page_id":"` + page.ID.String() + `","version_id":"` + revisions[0].ID.String() + `","state":"before"}`

	a, approval := requestWikiApproval(t, f, wikiRestoreAction, restore)
	resolved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Nil(t, resolved.Error)
	current, err := wiki.Get(f.ctx, f.ws, scope, page.ID)
	require.NoError(t, err)
	require.Equal(t, "<p>v1</p>", current.Content)

	// Someone edits after the restore was requested: approving fails as a
	// conflict and keeps their text.
	a, approval = requestWikiApproval(t, f, wikiRestoreAction, restore)
	v4 := "<p>someone else</p>"
	_, err = wiki.Update(f.ctx, f.ws, scope, page.ID, f.user, dto.UpdateWikiPageRequest{Content: &v4, Version: current.Version})
	require.NoError(t, err)
	resolved, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.NotNil(t, resolved.Error)
	require.Contains(t, *resolved.Error, "changed after this was requested")
	current, err = wiki.Get(f.ctx, f.ws, scope, page.ID)
	require.NoError(t, err)
	require.Equal(t, v4, current.Content)
}

func TestWikiRegistryDisableNeedsApprovalAndWriteAccess(t *testing.T) {
	f, wiki, scope := wikiApprovalFixture(t)
	on := true
	_, err := wiki.UpdateSettings(f.ctx, f.ws, scope, f.user, &on, nil)
	require.NoError(t, err)

	a, approval := requestWikiApproval(t, f, wikiDisableRegistryAction, `{"team_key":"ENG"}`)
	settings, err := wiki.Settings(f.ctx, f.ws, scope)
	require.NoError(t, err)
	require.True(t, settings.RegistryEnabled, "still on until approved")

	// The role is checked again when approving.
	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	resolved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	if err == nil {
		require.NotNil(t, resolved.Error, "a guest cannot carry out the approval")
	}
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	settings, err = wiki.Settings(f.ctx, f.ws, scope)
	require.NoError(t, err)
	require.True(t, settings.RegistryEnabled)

	a, approval = requestWikiApproval(t, f, wikiDisableRegistryAction, `{"team_key":"ENG"}`)
	resolved, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Nil(t, resolved.Error)
	settings, err = wiki.Settings(f.ctx, f.ws, scope)
	require.NoError(t, err)
	require.False(t, settings.RegistryEnabled)
}

func TestDestructiveWikiToolsAlwaysAskForApproval(t *testing.T) {
	for _, name := range []string{wikiDeleteAction, wikiRestoreAction, wikiDisableRegistryAction} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok, name)
		require.True(t, tool.approval, name)
		require.False(t, tool.write, name)
	}
}
