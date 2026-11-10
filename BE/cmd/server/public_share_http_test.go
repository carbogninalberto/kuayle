package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/handler"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// The unauthenticated share endpoint over real HTTP and PostgreSQL: query
// parameters cannot probe fields the link hides or reach issues its stored
// filters exclude, neither through the rows nor through total_count.
func TestPublicShareIssuesHTTP(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	owner, assignee, ws, team := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for id, email := range map[uuid.UUID]string{
		owner:    "owner-" + owner.String() + "@example.test",
		assignee: "quokkamail-" + assignee.String() + "@example.test",
	} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Public share','hash')`, id, email)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Public share',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	for user, role := range map[uuid.UUID]string{owner: "owner", assignee: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Public share','PUB')`, team, ws)
	require.NoError(t, err)
	backlog, done := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position) VALUES($1,$2,'Backlog','backlog','backlog',0),($3,$2,'Done','done','completed',1)`, backlog, team, done)
	require.NoError(t, err)
	shipped, queued := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,status,status_id,creator_id,assignee_id) VALUES
		($1,$3,$4,1,'PUB-1','Shipped feature','<p>internal wombatnote</p>','done',$5,$7,$8),
		($2,$3,$4,2,'PUB-2','Queued feature',NULL,'backlog',$6,$7,NULL)`, shipped, queued, ws, team, done, backlog, owner, assignee)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issue_assignees(issue_id,user_id) VALUES($1,$2)`, shipped, assignee)
	require.NoError(t, err)

	views := repository.NewViewRepository(db)
	svc := service.NewSharedLinkService(repository.NewSharedLinkRepository(db), repository.NewWorkspaceRepository(db), repository.NewTeamRepository(db), repository.NewProjectRepository(db), views, repository.NewIssueRepository(db), repository.NewUserRepository(db), repository.NewTeamStatusRepository(db), "public-share-http-test-secret")
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	link := func(scope string, scopeID uuid.UUID, filters string) string {
		t.Helper()
		id := scopeID.String()
		created, err := svc.Create(ownerCtx, ws, owner, dto.CreateSharedLinkRequest{Scope: scope, ScopeID: &id, Filters: json.RawMessage(filters)})
		require.NoError(t, err)
		return created.Token
	}
	view := func(filters string) uuid.UUID {
		t.Helper()
		v := &domain.View{ID: uuid.New(), WorkspaceID: ws, CreatorID: owner, Name: "Public share", Filters: json.RawMessage(filters), IsShared: true}
		require.NoError(t, views.Create(ownerCtx, v))
		return v.ID
	}

	e := echo.New()
	registerRoutes(e, &appHandlers{sharedLink: handler.NewSharedLinkHandler(svc, "https://kuayle.test")}, testMiddleware(noopMiddleware()))
	server := httptest.NewServer(e)
	defer server.Close()
	// No cookie and no Authorization header: this is the anonymous visitor.
	list := func(t *testing.T, token string, query neturl.Values) ([]string, int) {
		t.Helper()
		resp, err := server.Client().Get(server.URL + "/api/public/share/" + token + "/issues?" + query.Encode())
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s", data)
		require.NotContains(t, string(data), "quokkamail", "emails never leave the public endpoint")
		var body struct {
			Data []struct {
				Title       string  `json:"title"`
				Description *string `json:"description"`
			} `json:"data"`
			TotalCount int `json:"total_count"`
		}
		require.NoError(t, json.Unmarshal(data, &body))
		titles := make([]string, len(body.Data))
		for i, issue := range body.Data {
			titles[i] = issue.Title
		}
		sort.Strings(titles)
		return titles, body.TotalCount
	}
	expect := func(t *testing.T, token string, query neturl.Values, want ...string) {
		t.Helper()
		titles, total := list(t, token, query)
		if len(want) == 0 {
			require.Empty(t, titles, "query %v", query)
		} else {
			require.Equal(t, want, titles, "query %v", query)
		}
		require.Equal(t, len(want), total, "total_count for query %v", query)
	}
	search := func(term string) neturl.Values { return neturl.Values{"search": {term}} }

	t.Run("an assignee email is not searchable", func(t *testing.T) {
		expect(t, link("team", team, `{}`), search("quokkamail"))
	})
	t.Run("a hidden description is not searchable", func(t *testing.T) {
		token := link("team", team, `{}`)
		expect(t, token, search("wombatnote"))
		expect(t, token, search("shipped"), "Shipped feature")
	})
	t.Run("search wildcards are literal", func(t *testing.T) {
		token := link("team", team, `{}`)
		expect(t, token, search("%"))
		expect(t, token, search("_"))
		expect(t, token, search("Sh%d"))
	})
	t.Run("a stored status cannot be replaced", func(t *testing.T) {
		token := link("team", team, `{"status":"done"}`)
		expect(t, token, neturl.Values{}, "Shipped feature")
		expect(t, token, neturl.Values{"status": {"backlog"}})
		expect(t, token, neturl.Values{"status": {"done"}}, "Shipped feature")
	})
	t.Run("a view's creator filter is enforced", func(t *testing.T) {
		expect(t, link("view", view(`{"creator":"`+assignee.String()+`"}`), `{}`), neturl.Values{})
		expect(t, link("view", view(`{"creator":"`+owner.String()+`"}`), `{}`), neturl.Values{}, "Queued feature", "Shipped feature")
	})
	t.Run("a view with non-flat filters shows nothing", func(t *testing.T) {
		expect(t, link("view", view(`{"status":["done"]}`), `{}`), neturl.Values{})
	})
}
