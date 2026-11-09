package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// Publishing over real HTTP and PostgreSQL: only writers publish, public
// links need the workspace's share role and a non-private team, a public
// link shows only its page (and nested pages when asked), and unpublishing or
// rotating kills the old link at once.
func TestWikiPublishHTTP(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, member, guest := uuid.New(), uuid.New(), uuid.New()
	ws := uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Publisher','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Publish',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	for user, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}

	workspaces := repository.NewWorkspaceRepository(db)
	teams := repository.NewTeamRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	wikiRepo := repository.NewWikiRepository(db)
	publishSvc := service.NewWikiPublishService(wikiRepo, repository.NewSharedLinkRepository(db), workspaces, "publish-secret")
	handlers := &appHandlers{
		workspace:   handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, repository.NewUserRepository(db), hub)),
		team:        handler.NewTeamHandler(service.NewTeamService(teams, repository.NewTeamStatusRepository(db), hub)),
		project:     handler.NewProjectHandler(service.NewProjectService(repository.NewProjectRepository(db))),
		wiki:        handler.NewWikiHandler(service.NewWikiService(repository.NewWikiRepository(db)), &fakeWikiAI{}),
		wikiPublish: handler.NewWikiPublishHandler(publishSvc, "https://kuayle.test"),
	}
	secret := "wiki-publish-http-test-secret"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, handlers, middleware)
	server := httptest.NewServer(e)
	defer server.Close()

	jwt := func(user uuid.UUID) string {
		token, err := jwtpkg.GenerateAccessToken(user, secret)
		require.NoError(t, err)
		return token
	}
	request := func(token, method, path, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "access_token", Value: token})
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, want, resp.StatusCode, "%s %s: %s", method, path, data)
		return data
	}
	type node struct {
		ID       string  `json:"id"`
		ParentID *string `json:"parent_id"`
		Position int     `json:"position"`
		Kind     string  `json:"kind"`
	}
	id := func(data []byte) string {
		var body struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(data, &body))
		return body.ID
	}
	ownerJWT, memberJWT, guestJWT := jwt(owner), jwt(member), jwt(guest)
	base := "/api/workspaces/" + ws.String()
	team := id(request(ownerJWT, "POST", base+"/teams", `{"name":"Docs","key":"DOC"}`, http.StatusCreated))
	private := id(request(ownerJWT, "POST", base+"/teams", `{"name":"Secret","key":"SEC","is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusCreated))
	wiki := base + "/teams/" + team + "/wiki"
	asset := uuid.New()
	root := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Handbook","content":"<p>Hello</p><img src=\"/api/workspaces/`+ws.String()+`/assets/`+asset.String()+`\">"}`, http.StatusCreated))
	child := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Chapter","content":"<p>Inside</p>","parent_id":"`+root+`"}`, http.StatusCreated))
	sibling := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Internal notes","content":"<p>secret sibling</p>"}`, http.StatusCreated))

	type publication struct {
		Mode     string  `json:"mode"`
		Children bool    `json:"children"`
		URL      *string `json:"url"`
	}
	publish := func(token, page, body string, want int) publication {
		t.Helper()
		var state publication
		data := request(token, "PUT", wiki+"/"+page+"/publish", body, want)
		if want == http.StatusOK {
			require.NoError(t, json.Unmarshal(data, &state))
		}
		return state
	}
	type publicPage struct {
		ID       string  `json:"id"`
		ParentID *string `json:"parent_id"`
		Title    string  `json:"title"`
		Content  string  `json:"content"`
	}
	type publicWiki struct {
		WorkspaceName string       `json:"workspace_name"`
		RootID        string       `json:"root_id"`
		Pages         []publicPage `json:"pages"`
	}
	anonymous := func(link string, want int) publicWiki {
		t.Helper()
		token := link[strings.LastIndex(link, "/")+1:]
		resp, err := http.Get(server.URL + "/api/public/wiki/" + token)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		require.Equal(t, want, resp.StatusCode, string(data))
		var out publicWiki
		if want == http.StatusOK {
			require.NoError(t, json.Unmarshal(data, &out))
		}
		return out
	}

	// Internal publishing is for writers; nothing becomes public.
	state := publish(memberJWT, root, `{"mode":"internal"}`, http.StatusOK)
	require.Equal(t, publication{Mode: "internal"}, state)
	publish(guestJWT, root, `{"mode":"internal"}`, http.StatusForbidden)
	publish(memberJWT, root, `{"mode":"everyone"}`, http.StatusBadRequest)
	var read publication
	require.NoError(t, json.Unmarshal(request(guestJWT, "GET", wiki+"/"+root+"/publish", "", http.StatusOK), &read))
	require.Equal(t, "internal", read.Mode)

	// Public links follow the workspace's share role (admins by default).
	publish(memberJWT, root, `{"mode":"public"}`, http.StatusForbidden)
	state = publish(ownerJWT, root, `{"mode":"public"}`, http.StatusOK)
	require.NotNil(t, state.URL)
	require.True(t, strings.HasPrefix(*state.URL, "https://kuayle.test/share/wiki/"))
	link := *state.URL

	// The link shows the page alone: no nested or sibling pages, no authors.
	shown := anonymous(link, http.StatusOK)
	require.Equal(t, root, shown.RootID)
	require.Len(t, shown.Pages, 1)
	require.Equal(t, "Handbook", shown.Pages[0].Title)
	require.Nil(t, shown.Pages[0].ParentID)
	require.Contains(t, shown.Pages[0].Content, "/api/public/assets/", "files are served through signed public links")
	require.NotContains(t, shown.Pages[0].Content, "/api/workspaces/")
	require.True(t, publishSvc.PublicPageReachable(context.Background(), ws, uuid.MustParse(root)))
	require.False(t, publishSvc.PublicPageReachable(context.Background(), ws, uuid.MustParse(child)))

	// Including nested pages adds the chapter, never the sibling.
	state = publish(ownerJWT, root, `{"mode":"public","children":true}`, http.StatusOK)
	require.Equal(t, link, *state.URL, "changing what is included keeps the link")
	shown = anonymous(link, http.StatusOK)
	titles := []string{}
	for _, page := range shown.Pages {
		titles = append(titles, page.Title)
		require.NotContains(t, page.Content, "secret sibling")
	}
	require.Equal(t, []string{"Handbook", "Chapter"}, titles)
	require.Equal(t, root, *shown.Pages[1].ParentID)
	require.True(t, publishSvc.PublicPageReachable(context.Background(), ws, uuid.MustParse(child)))
	require.False(t, publishSvc.PublicPageReachable(context.Background(), ws, uuid.MustParse(sibling)))

	// Rotating replaces the token; the old one is dead immediately.
	state = publish(ownerJWT, root, `{"mode":"public","children":true,"rotate":true}`, http.StatusOK)
	require.NotEqual(t, link, *state.URL)
	anonymous(link, http.StatusNotFound)
	link = *state.URL
	anonymous(link, http.StatusOK)

	// Unpublishing kills the link and the signed files.
	publish(ownerJWT, root, `{"mode":"off"}`, http.StatusOK)
	anonymous(link, http.StatusNotFound)
	require.False(t, publishSvc.PublicPageReachable(context.Background(), ws, uuid.MustParse(root)))
	anonymous("https://kuayle.test/share/wiki/not-a-token", http.StatusNotFound)

	// A private team's page can never be public, and others cannot see it at all.
	hidden := id(request(ownerJWT, "POST", base+"/teams/"+private+"/wiki", `{"title":"Hidden"}`, http.StatusCreated))
	request(ownerJWT, "PUT", base+"/teams/"+private+"/wiki/"+hidden+"/publish", `{"mode":"public"}`, http.StatusConflict)
	request(memberJWT, "PUT", base+"/teams/"+private+"/wiki/"+hidden+"/publish", `{"mode":"internal"}`, http.StatusNotFound)
	request(memberJWT, "GET", base+"/teams/"+private+"/wiki/"+hidden+"/publish", "", http.StatusNotFound)
	var mode string
	require.NoError(t, db.Get(&mode, `SELECT publish_mode FROM wiki_pages WHERE id=$1`, hidden))
	require.Equal(t, "off", mode, "a refused public link leaves the page unpublished")
}
