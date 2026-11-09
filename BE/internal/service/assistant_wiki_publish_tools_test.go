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

// wikiPublishFixture wires the wiki, its tree and publishing into the
// assistant's tools and approvals, as in production.
func wikiPublishFixture(t *testing.T) (toolFixture, *WikiService, *WikiPublishService, domain.WikiScope) {
	t.Helper()
	f := newToolFixture(t)
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	wikiRepo := repository.NewWikiRepository(f.db)
	wiki := NewWikiService(wikiRepo)
	workspaces := repository.NewWorkspaceRepository(f.db)
	publish := NewWikiPublishService(wikiRepo, repository.NewSharedLinkRepository(f.db), workspaces, "assistant-publish-key")
	f.service.tools.SetWiki(wiki, repository.NewProjectRepository(f.db))
	f.service.tools.SetWikiPublishing(publish, workspaces)
	approvals := NewAssistantApprovals(f.repo, &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)})
	approvals.SetWiki(wiki, workspaces)
	approvals.SetWikiPublishing(publish, workspaces)
	f.service.SetApprovals(approvals)
	return f, wiki, publish, domain.TeamWiki(team)
}

func runWikiTool(t *testing.T, f toolFixture, name string, args map[string]any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	result, _, err := f.service.tools.run(f.ctx, f.ws, f.user, name, string(raw))
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(encoded, &out))
	return out, nil
}

// The assistant organises the tree with the same validated moves as the UI,
// and hears the server's reason when a move is refused.
func TestAssistantWikiTreeTools(t *testing.T) {
	f, wiki, _, scope := wikiPublishFixture(t)
	folder, err := runWikiTool(t, f, "create_wiki_page", map[string]any{"team_key": "ENG", "title": "Guides", "kind": "folder"})
	require.NoError(t, err)
	child, err := runWikiTool(t, f, "create_wiki_page", map[string]any{"team_key": "ENG", "title": "Setup", "parent_id": folder["page_id"]})
	require.NoError(t, err)
	loose, err := runWikiTool(t, f, "create_wiki_page", map[string]any{"team_key": "ENG", "title": "FAQ"})
	require.NoError(t, err)

	listed, err := runWikiTool(t, f, "list_wiki_pages", map[string]any{"team_key": "ENG"})
	require.NoError(t, err)
	byID := map[string]map[string]any{}
	for _, page := range listed["pages"].([]any) {
		entry := page.(map[string]any)
		byID[entry["page_id"].(string)] = entry
	}
	require.Equal(t, "folder", byID[folder["page_id"].(string)]["kind"])
	require.Equal(t, folder["page_id"], byID[child["page_id"].(string)]["parent_id"])

	moved, err := runWikiTool(t, f, "move_wiki_page", map[string]any{"team_key": "ENG", "page_id": loose["page_id"], "parent_id": folder["page_id"], "index": 0})
	require.NoError(t, err)
	require.Equal(t, folder["page_id"], moved["parent_id"])
	require.EqualValues(t, 0, moved["position"])

	_, err = runWikiTool(t, f, "move_wiki_page", map[string]any{"team_key": "ENG", "page_id": folder["page_id"], "parent_id": child["page_id"]})
	require.ErrorContains(t, err, "cannot move into itself")
	_, err = runWikiTool(t, f, "move_wiki_page", map[string]any{"team_key": "ENG", "page_id": child["page_id"], "parent_id": uuid.NewString()})
	require.ErrorContains(t, err, "wiki page not found", "a parent outside this wiki is not found")
	_, err = runWikiTool(t, f, "move_wiki_page", map[string]any{"team_key": "SEC", "page_id": child["page_id"]})
	require.Error(t, err, "a wiki the user cannot see")

	nodes, err := wiki.Tree(f.ctx, f.ws, scope)
	require.NoError(t, err)
	for _, node := range nodes {
		if node.ID.String() == folder["page_id"] {
			require.Nil(t, node.ParentID, "the refused moves changed nothing")
		}
	}

	_, err = f.db.Exec(`UPDATE workspace_members SET role='guest' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	_, err = runWikiTool(t, f, "move_wiki_page", map[string]any{"team_key": "ENG", "page_id": child["page_id"], "parent_id": nil})
	require.Error(t, err, "guests cannot move pages")
	_, err = runWikiTool(t, f, "publish_wiki_page", map[string]any{"team_key": "ENG", "page_id": child["page_id"], "mode": "internal"})
	require.Error(t, err, "guests cannot publish")
}

// Internal publishing is an ordinary write; anything anonymous readers see
// changing (going public, a new link, coming off the web) waits for approval.
func TestAssistantWikiPublishingNeedsApprovalForPublicLinks(t *testing.T) {
	f, wiki, publish, scope := wikiPublishFixture(t)
	page, err := wiki.Create(f.ctx, f.ws, scope, f.user, dto.CreateWikiPageRequest{Title: "Handbook", Content: "<p>Hi</p>"})
	require.NoError(t, err)
	state := func() *domain.WikiPublication {
		t.Helper()
		s, err := publish.State(f.ctx, f.ws, scope, page.ID)
		require.NoError(t, err)
		return s
	}
	args := func(extra string) string {
		return `{"team_key":"ENG","page_id":"` + page.ID.String() + `"` + extra + `}`
	}

	out, err := runWikiTool(t, f, "publish_wiki_page", map[string]any{"team_key": "ENG", "page_id": page.ID.String(), "mode": "internal"})
	require.NoError(t, err)
	require.Equal(t, "internal", out["mode"])
	_, err = runWikiTool(t, f, "publish_wiki_page", map[string]any{"team_key": "ENG", "page_id": page.ID.String(), "mode": "public"})
	require.Error(t, err, "publish_wiki_page never makes a page public")

	// Members cannot share publicly while the workspace keeps that to admins.
	_, err = f.service.tools.prepareWikiApproval(f.ctx, f.ws, f.user, wikiShareAction, args(`,"mode":"public"`))
	require.ErrorContains(t, err, "cannot share pages publicly")
	_, err = f.db.Exec(`UPDATE workspaces SET share_link_min_role='member' WHERE id=$1`, f.ws)
	require.NoError(t, err)

	a, approval := requestWikiApproval(t, f, wikiShareAction, args(`,"mode":"public","children":true`))
	require.Equal(t, "internal", state().Mode, "nothing is public before approval")
	var payload domain.WikiApprovalPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, "Handbook", payload.Title)
	require.Equal(t, "ENG", payload.ScopeName)
	require.Equal(t, "public", payload.PublishMode)
	require.Equal(t, "internal", payload.PublishedFrom)
	resolved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Nil(t, resolved.Error)
	public := state()
	require.Equal(t, "public", public.Mode)
	require.True(t, public.Children)
	require.NotNil(t, public.Token)

	// Rotating needs approval too, and the old token stops resolving.
	a, approval = requestWikiApproval(t, f, wikiShareAction, args(`,"mode":"public","rotate":true`))
	require.Equal(t, *public.Token, *state().Token)
	_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.NotEqual(t, *public.Token, *state().Token)
	_, err = publish.ResolvePublic(f.ctx, *public.Token)
	require.ErrorIs(t, err, ErrWikiNotFound)

	// Taking a public page off the web is not an ordinary write either.
	_, err = runWikiTool(t, f, "publish_wiki_page", map[string]any{"team_key": "ENG", "page_id": page.ID.String(), "mode": "off"})
	require.ErrorContains(t, err, "share_wiki_page")
	a, approval = requestWikiApproval(t, f, wikiShareAction, args(`,"mode":"off"`))
	require.Equal(t, "public", state().Mode)
	_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "off", state().Mode)
	require.Nil(t, state().Token)

	// share_wiki_page is only for public links.
	_, err = f.service.tools.prepareWikiApproval(f.ctx, f.ws, f.user, wikiShareAction, args(`,"mode":"internal"`))
	require.ErrorContains(t, err, "not public")
}

// The share role is checked again when approving: a role lowered in the
// meantime cannot carry out a pending public link.
func TestAssistantWikiShareRechecksRoleOnApproval(t *testing.T) {
	f, wiki, publish, scope := wikiPublishFixture(t)
	page, err := wiki.Create(f.ctx, f.ws, scope, f.user, dto.CreateWikiPageRequest{Title: "Plan"})
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE workspaces SET share_link_min_role='member' WHERE id=$1`, f.ws)
	require.NoError(t, err)
	a, approval := requestWikiApproval(t, f, wikiShareAction, `{"team_key":"ENG","page_id":"`+page.ID.String()+`","mode":"public"}`)
	_, err = f.db.Exec(`UPDATE workspaces SET share_link_min_role='admin' WHERE id=$1`, f.ws)
	require.NoError(t, err)
	resolved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.NotNil(t, resolved.Error)
	s, err := publish.State(f.ctx, f.ws, scope, page.ID)
	require.NoError(t, err)
	require.Equal(t, "off", s.Mode)
}

func TestWikiTreeAndPublishToolsAreClassified(t *testing.T) {
	for name, want := range map[string][2]bool{"move_wiki_page": {true, false}, "publish_wiki_page": {true, false}, wikiShareAction: {false, true}} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok, name)
		require.Equal(t, want[0], tool.write, name)
		require.Equal(t, want[1], tool.approval, name)
		require.True(t, tool.publish || name == "move_wiki_page", name)
		require.True(t, json.Valid([]byte(tool.parameters)), name)
	}
	require.False(t, NewAssistantTools(nil, nil, nil, nil).available(assistantTool{wiki: true, publish: true}), "unavailable until SetWikiPublishing")
}
