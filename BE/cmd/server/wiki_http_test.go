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
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Team and project wikis over real HTTP, auth, workspace middleware and
// PostgreSQL: content persists, guests are read-only, and private teams,
// other scopes and other workspaces never see a page.
func TestWikiHTTPAccessAndPersistence(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, member, guest, outsider, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ws, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest, outsider, stranger} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Wiki tester','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Wiki',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
	}
	for user, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}
	// stranger belongs only to the other workspace.
	for user, role := range map[uuid.UUID]string{owner: "owner", stranger: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, other, user, role)
		require.NoError(t, err)
	}

	users := repository.NewUserRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	teams := repository.NewTeamRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	handlers := &appHandlers{
		workspace: handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, users, hub)),
		team:      handler.NewTeamHandler(service.NewTeamService(teams, repository.NewTeamStatusRepository(db), hub)),
		project:   handler.NewProjectHandler(service.NewProjectService(repository.NewProjectRepository(db))),
		wiki:      handler.NewWikiHandler(service.NewWikiService(repository.NewWikiRepository(db))),
	}
	secret := "wiki-http-test-secret"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, handlers, middleware)
	server := httptest.NewServer(e)
	defer server.Close()

	jwt := func(user uuid.UUID) string {
		t.Helper()
		token, err := jwtpkg.GenerateAccessToken(user, secret)
		require.NoError(t, err)
		return token
	}
	pat := func(user uuid.UUID, scopes ...string) string {
		t.Helper()
		raw, hash, prefix, err := domain.GenerateToken()
		require.NoError(t, err)
		require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "Wiki", TokenHash: hash, TokenPrefix: prefix, Scopes: pq.StringArray(scopes)}))
		return raw
	}
	request := func(token, method, path, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(token, domain.TokenPrefix) {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.AddCookie(&http.Cookie{Name: "access_token", Value: token})
		}
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, want, resp.StatusCode, "%s %s: %s", method, path, data)
		return data
	}
	type page struct {
		ID            string  `json:"id"`
		Title         string  `json:"title"`
		Content       string  `json:"content"`
		Version       int     `json:"version"`
		TeamID        *string `json:"team_id"`
		ProjectID     *string `json:"project_id"`
		UpdatedByName *string `json:"updated_by_name"`
	}
	decode := func(data []byte, target any) { t.Helper(); require.NoError(t, json.Unmarshal(data, target)) }
	created := func(data []byte) string {
		var body struct {
			ID string `json:"id"`
		}
		decode(data, &body)
		return body.ID
	}

	ownerJWT, memberJWT, guestJWT := jwt(owner), jwt(member), jwt(guest)
	base := "/api/workspaces/" + ws.String()
	publicTeam := created(request(ownerJWT, "POST", base+"/teams", `{"name":"Docs team","key":"DOC"}`, http.StatusCreated))
	privateTeam := created(request(ownerJWT, "POST", base+"/teams", `{"name":"Secret team","key":"SEC","is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusCreated))
	publicProject := created(request(ownerJWT, "POST", base+"/projects", `{"name":"Open project","team_id":"`+publicTeam+`"}`, http.StatusCreated))
	privateProject := created(request(ownerJWT, "POST", base+"/projects", `{"name":"Hidden project","team_id":"`+privateTeam+`"}`, http.StatusCreated))
	teamWiki := base + "/teams/" + publicTeam + "/wiki"
	privateWiki := base + "/teams/" + privateTeam + "/wiki"
	projectWiki := base + "/projects/" + publicProject + "/wiki"
	privateProjectWiki := base + "/projects/" + privateProject + "/wiki"

	// An empty wiki is a successful empty list, not an error.
	require.JSONEq(t, `[]`, string(request(memberJWT, "GET", teamWiki, "", http.StatusOK)))

	// Create, then reopen with content preserved; script content is stripped.
	content := `<h2>Release checklist</h2><ul><li><p>Tag</p></li></ul><script>alert(1)</script>`
	body, _ := json.Marshal(map[string]string{"title": "Releases", "content": content})
	var createdPage page
	decode(request(memberJWT, "POST", teamWiki, string(body), http.StatusCreated), &createdPage)
	require.Equal(t, 1, createdPage.Version)
	require.Equal(t, publicTeam, *createdPage.TeamID)
	require.Nil(t, createdPage.ProjectID)
	require.Equal(t, "Wiki tester", *createdPage.UpdatedByName)
	pagePath := teamWiki + "/" + createdPage.ID
	var reopened page
	decode(request(guestJWT, "GET", pagePath, "", http.StatusOK), &reopened)
	require.Equal(t, "Releases", reopened.Title)
	require.Equal(t, `<h2>Release checklist</h2><ul><li><p>Tag</p></li></ul>`, reopened.Content)
	var listed []page
	decode(request(guestJWT, "GET", teamWiki, "", http.StatusOK), &listed)
	require.Len(t, listed, 1)
	require.Equal(t, "Releases", listed[0].Title)
	require.Empty(t, listed[0].Content, "the list omits page bodies")

	request(memberJWT, "POST", teamWiki, `{"title":"   "}`, http.StatusBadRequest)

	// Guests and read-only tokens can read but never write.
	request(guestJWT, "POST", teamWiki, `{"title":"Guest page"}`, http.StatusForbidden)
	request(guestJWT, "PATCH", pagePath, `{"content":"<p>guest</p>","version":1}`, http.StatusForbidden)
	request(guestJWT, "DELETE", pagePath, "", http.StatusForbidden)
	readPAT := pat(member, domain.PermWikiRead)
	request(readPAT, "GET", pagePath, "", http.StatusOK)
	request(readPAT, "PATCH", pagePath, `{"content":"<p>token</p>","version":1}`, http.StatusForbidden)
	request(pat(member, domain.PermTeamsRead), "GET", teamWiki, "", http.StatusForbidden)
	request(pat(guest, domain.PermWikiRead, domain.PermWikiWrite), "POST", teamWiki, `{"title":"Guest token"}`, http.StatusForbidden)

	// Edits are versioned: a stale editor gets 409 and the saved text survives.
	var edited page
	decode(request(ownerJWT, "PATCH", pagePath, `{"content":"<p>Second draft</p>","version":1}`, http.StatusOK), &edited)
	require.Equal(t, 2, edited.Version)
	require.Equal(t, "Releases", edited.Title)
	request(memberJWT, "PATCH", pagePath, `{"content":"<p>Stale</p>","version":1}`, http.StatusConflict)
	decode(request(memberJWT, "GET", pagePath, "", http.StatusOK), &reopened)
	require.Equal(t, "<p>Second draft</p>", reopened.Content)
	require.Equal(t, 2, reopened.Version)

	// Private team and private-team project wikis are invisible to non-members,
	// for reads and writes alike; the response does not reveal that they exist.
	privatePage := created(request(ownerJWT, "POST", privateWiki, `{"title":"Secrets","content":"<p>classified</p>"}`, http.StatusCreated))
	privateProjectPage := created(request(ownerJWT, "POST", privateProjectWiki, `{"title":"Hidden plan"}`, http.StatusCreated))
	for _, path := range []string{privateWiki, privateProjectWiki} {
		request(memberJWT, "GET", path, "", http.StatusNotFound)
		request(guestJWT, "GET", path, "", http.StatusNotFound)
		request(memberJWT, "POST", path, `{"title":"Planted"}`, http.StatusNotFound)
	}
	for _, path := range []string{privateWiki + "/" + privatePage, privateProjectWiki + "/" + privateProjectPage} {
		request(memberJWT, "GET", path, "", http.StatusNotFound)
		request(memberJWT, "PATCH", path, `{"title":"Defaced","version":1}`, http.StatusNotFound)
		request(memberJWT, "DELETE", path, "", http.StatusNotFound)
	}
	// Membership is evaluated live.
	request(ownerJWT, "POST", base+"/teams/"+privateTeam+"/members", `{"user_id":"`+member.String()+`"}`, http.StatusOK)
	decode(request(memberJWT, "GET", privateWiki+"/"+privatePage, "", http.StatusOK), &reopened)
	require.Equal(t, "<p>classified</p>", reopened.Content)
	request(memberJWT, "GET", privateProjectWiki+"/"+privateProjectPage, "", http.StatusOK)
	request(ownerJWT, "DELETE", base+"/teams/"+privateTeam+"/members/"+member.String(), "", http.StatusOK)
	request(memberJWT, "GET", privateWiki+"/"+privatePage, "", http.StatusNotFound)

	// Pages are reachable only through their own team or project.
	projectPage := created(request(memberJWT, "POST", projectWiki, `{"title":"Project notes"}`, http.StatusCreated))
	request(ownerJWT, "GET", privateWiki+"/"+createdPage.ID, "", http.StatusNotFound)
	request(ownerJWT, "PATCH", privateWiki+"/"+createdPage.ID, `{"title":"Moved","version":2}`, http.StatusNotFound)
	request(ownerJWT, "GET", projectWiki+"/"+createdPage.ID, "", http.StatusNotFound)
	request(ownerJWT, "GET", teamWiki+"/"+projectPage, "", http.StatusNotFound)
	request(ownerJWT, "GET", base+"/projects/"+publicTeam+"/wiki", "", http.StatusNotFound)
	decode(request(ownerJWT, "GET", projectWiki, "", http.StatusOK), &listed)
	require.Len(t, listed, 1)
	require.Equal(t, "Project notes", listed[0].Title)

	// Outsiders and members of another workspace are denied.
	request(jwt(outsider), "GET", teamWiki, "", http.StatusForbidden)
	request(jwt(stranger), "GET", teamWiki, "", http.StatusForbidden)
	otherBase := "/api/workspaces/" + other.String()
	request(jwt(stranger), "GET", otherBase+"/teams/"+publicTeam+"/wiki", "", http.StatusNotFound)
	request(jwt(stranger), "GET", otherBase+"/teams/"+publicTeam+"/wiki/"+createdPage.ID, "", http.StatusNotFound)
	request(jwt(stranger), "POST", otherBase+"/projects/"+publicProject+"/wiki", `{"title":"Cross"}`, http.StatusNotFound)
	request(ownerJWT, "GET", otherBase+"/teams/"+publicTeam+"/wiki/"+createdPage.ID, "", http.StatusNotFound)

	// Deleting removes the page; deleting a project or team removes its wiki.
	request(memberJWT, "DELETE", pagePath, "", http.StatusOK)
	request(memberJWT, "GET", pagePath, "", http.StatusNotFound)
	request(memberJWT, "DELETE", pagePath, "", http.StatusNotFound)
	var count int
	request(ownerJWT, "DELETE", base+"/projects/"+privateProject, "", http.StatusOK)
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM wiki_pages WHERE project_id=$1`, privateProject))
	require.Zero(t, count)
	request(ownerJWT, "DELETE", base+"/teams/"+privateTeam, "", http.StatusOK)
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM wiki_pages WHERE team_id=$1`, privateTeam))
	require.Zero(t, count)
}
