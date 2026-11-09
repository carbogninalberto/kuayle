package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// wikiToolTeams lists only the teams the user can see, like the repository.
type wikiToolTeams struct {
	repository.TeamRepo
	teams []domain.Team
}

func (f wikiToolTeams) ListByWorkspace(context.Context, uuid.UUID) ([]domain.Team, error) {
	return f.teams, nil
}

type wikiToolProjects []domain.Project

func (f wikiToolProjects) ListByWorkspace(context.Context, uuid.UUID) ([]domain.Project, error) {
	return f, nil
}

type wikiToolFixture struct {
	tools *AssistantTools
	repo  *fakeWikiRepo
	ws    uuid.UUID
	user  uuid.UUID
	team  domain.WikiScope
}

func newWikiToolFixture(role string) *wikiToolFixture {
	ws, user := uuid.New(), uuid.New()
	team, project := domain.Team{ID: uuid.New(), Key: "ENG"}, domain.Project{ID: uuid.New(), Name: "Launch"}
	repo := newFakeWikiRepo(domain.TeamWiki(team.ID), domain.ProjectWiki(project.ID))
	tools := NewAssistantTools(nil, wikiToolTeams{teams: []domain.Team{team}}, memberFake{role: role}, nil)
	tools.SetWiki(NewWikiService(repo), wikiToolProjects{project})
	return &wikiToolFixture{tools: tools, repo: repo, ws: ws, user: user, team: domain.TeamWiki(team.ID)}
}

func (f *wikiToolFixture) run(t *testing.T, name string, args map[string]any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	result, _, err := f.tools.run(context.Background(), f.ws, f.user, name, string(raw))
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(encoded, &out))
	return out, nil
}

func TestAssistantWikiToolsWriteReadAndAppend(t *testing.T) {
	f := newWikiToolFixture(domain.RoleMember)

	created, err := f.run(t, "create_wiki_page", map[string]any{"team_key": "eng", "title": "Runbook", "content": "## Deploy\n\nRun make deploy."})
	require.NoError(t, err)
	pageID := created["page_id"].(string)
	require.Equal(t, "Runbook", created["title"])

	read, err := f.run(t, "get_wiki_page", map[string]any{"team_key": "ENG", "page_id": pageID})
	require.NoError(t, err)
	require.Contains(t, read["content"], "Run make deploy.")
	require.NotContains(t, read["content"], "<h2>", "pages are read as text")

	appended, err := f.run(t, "update_wiki_page", map[string]any{"team_key": "ENG", "page_id": pageID, "mode": "append", "content": "**Rollback**: redeploy the previous tag."})
	require.NoError(t, err)
	require.EqualValues(t, 2, appended["version"])
	stored := f.repo.pages[uuid.MustParse(pageID)]
	require.Contains(t, stored.Content, "<h2>Deploy</h2>")
	require.Contains(t, stored.Content, "<strong>Rollback</strong>")

	_, err = f.run(t, "update_wiki_page", map[string]any{"team_key": "ENG", "page_id": pageID, "content": "stale", "version": 1})
	require.ErrorContains(t, err, "changed since you read it")
	_, err = f.run(t, "update_wiki_page", map[string]any{"team_key": "ENG", "page_id": pageID, "content": "no version"})
	require.ErrorContains(t, err, "version is required")

	listed, err := f.run(t, "list_wiki_pages", map[string]any{"project": "launch"})
	require.NoError(t, err)
	require.Empty(t, listed["pages"], "pages stay in their own wiki")

	_, err = f.run(t, "append_wiki_registry", map[string]any{"team_key": "ENG", "text": "KUA-20 implemented"})
	require.ErrorContains(t, err, "turned off")
	f.repo.registry[f.team] = true
	entry, err := f.run(t, "append_wiki_registry", map[string]any{"team_key": "ENG", "text": "KUA-20 implemented"})
	require.NoError(t, err)
	require.NotEmpty(t, entry["registry_date"])
}

func TestAssistantWikiToolsRespectRolesAndScopes(t *testing.T) {
	guest := newWikiToolFixture(domain.RoleGuest)
	_, err := guest.run(t, "list_wiki_pages", map[string]any{"team_key": "ENG"})
	require.ErrorContains(t, err, "no longer have access")

	f := newWikiToolFixture(domain.RoleMember)
	for _, args := range []map[string]any{
		{"team_key": "SEC"},                      // a team the user cannot see
		{"project": "Hidden"},                    // a project the user cannot see
		{"team_key": "ENG", "project": "Launch"}, // ambiguous
		{},
	} {
		_, err := f.run(t, "list_wiki_pages", args)
		require.Error(t, err, args)
	}
	_, err = f.run(t, "get_wiki_page", map[string]any{"team_key": "ENG", "page_id": uuid.NewString()})
	require.ErrorContains(t, err, "wiki page not found")
	_, err = f.run(t, "list_wiki_pages", map[string]any{"team_key": "ENG", "title": "x"})
	require.ErrorContains(t, err, "invalid arguments", "tools accept only their own fields")
	require.Zero(t, f.repo.writes)
}

func TestAssistantWikiToolsAreRegisteredWrites(t *testing.T) {
	for name, write := range map[string]bool{"list_wiki_pages": false, "get_wiki_page": false, "create_wiki_page": true, "update_wiki_page": true, "append_wiki_registry": true} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok, name)
		require.Equal(t, write, tool.write, name)
		require.False(t, tool.approval, name)
		require.True(t, json.Valid([]byte(tool.parameters)), name)
	}
	require.False(t, NewAssistantTools(nil, nil, nil, nil).available(assistantTool{wiki: true}), "unavailable until SetWiki")
}
