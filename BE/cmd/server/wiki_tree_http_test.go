package main

import (
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

// The page tree over real HTTP and PostgreSQL: folders nest pages, moves
// reorder and renumber siblings, and cycles, over-deep nesting, other scopes
// and invisible wikis are refused. Deleting a folder lifts its pages.
func TestWikiTreeHTTPMovesAndValidation(t *testing.T) {
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
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Tree tester','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Tree',$2,$3)`, ws, ws.String(), owner)
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
	handlers := &appHandlers{
		workspace: handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, repository.NewUserRepository(db), hub)),
		team:      handler.NewTeamHandler(service.NewTeamService(teams, repository.NewTeamStatusRepository(db), hub)),
		project:   handler.NewProjectHandler(service.NewProjectService(repository.NewProjectRepository(db))),
		wiki:      handler.NewWikiHandler(service.NewWikiService(repository.NewWikiRepository(db)), &fakeWikiAI{}),
	}
	secret := "wiki-tree-http-test-secret"
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
	project := id(request(ownerJWT, "POST", base+"/projects", `{"name":"Launch","team_id":"`+team+`"}`, http.StatusCreated))
	wiki := base + "/teams/" + team + "/wiki"
	tree := func(token string) map[string]node {
		t.Helper()
		var nodes []node
		require.NoError(t, json.Unmarshal(request(token, "GET", wiki+"/tree", "", http.StatusOK), &nodes))
		out := map[string]node{}
		for _, n := range nodes {
			out[n.ID] = n
		}
		return out
	}
	parentOf := func(n node) string {
		if n.ParentID == nil {
			return ""
		}
		return *n.ParentID
	}

	folder := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Guides","kind":"folder"}`, http.StatusCreated))
	intro := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Intro","parent_id":"`+folder+`"}`, http.StatusCreated))
	setup := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Setup","parent_id":"`+folder+`"}`, http.StatusCreated))
	faq := id(request(memberJWT, "POST", wiki+"/tree", `{"title":"FAQ"}`, http.StatusCreated))
	request(memberJWT, "POST", wiki+"/tree", `{"title":"Odd","kind":"shelf"}`, http.StatusBadRequest)
	request(guestJWT, "POST", wiki+"/tree", `{"title":"Guest"}`, http.StatusForbidden)

	nodes := tree(guestJWT)
	require.Equal(t, "folder", nodes[folder].Kind)
	require.Equal(t, folder, parentOf(nodes[intro]))
	require.Equal(t, []int{0, 1}, []int{nodes[intro].Position, nodes[setup].Position})
	require.Equal(t, []int{0, 1}, []int{nodes[folder].Position, nodes[faq].Position})

	// Into the folder, first: the folder's pages shift, the top level closes up.
	request(memberJWT, "POST", wiki+"/"+faq+"/move", `{"parent_id":"`+folder+`","index":0}`, http.StatusOK)
	nodes = tree(memberJWT)
	require.Equal(t, folder, parentOf(nodes[faq]))
	require.Equal(t, []int{0, 1, 2}, []int{nodes[faq].Position, nodes[intro].Position, nodes[setup].Position})
	// Back out to the top level.
	request(memberJWT, "POST", wiki+"/"+setup+"/move", `{"parent_id":null,"index":0}`, http.StatusOK)
	nodes = tree(memberJWT)
	require.Equal(t, "", parentOf(nodes[setup]))
	require.Equal(t, []int{0, 1}, []int{nodes[setup].Position, nodes[folder].Position})
	require.Equal(t, []int{0, 1}, []int{nodes[faq].Position, nodes[intro].Position})

	// A folder cannot move into itself or anything inside it.
	request(memberJWT, "POST", wiki+"/"+folder+"/move", `{"parent_id":"`+intro+`","index":0}`, http.StatusBadRequest)
	request(memberJWT, "POST", wiki+"/"+folder+"/move", `{"parent_id":"`+folder+`","index":0}`, http.StatusBadRequest)
	// Nesting stops at six levels.
	chain := folder
	for i := 0; i < 5; i++ {
		chain = id(request(memberJWT, "POST", wiki+"/tree", `{"title":"Level","parent_id":"`+chain+`"}`, http.StatusCreated))
	}
	request(memberJWT, "POST", wiki+"/"+setup+"/move", `{"parent_id":"`+chain+`","index":0}`, http.StatusBadRequest)
	request(memberJWT, "POST", wiki+"/tree", `{"title":"Too deep","parent_id":"`+chain+`"}`, http.StatusBadRequest)

	// Parents must be in the same wiki; other wikis and invisible ones are not found.
	projectPage := id(request(memberJWT, "POST", base+"/projects/"+project+"/wiki", `{"title":"Project page"}`, http.StatusCreated))
	request(memberJWT, "POST", wiki+"/"+setup+"/move", `{"parent_id":"`+projectPage+`","index":0}`, http.StatusNotFound)
	request(memberJWT, "POST", base+"/projects/"+project+"/wiki/"+setup+"/move", `{"parent_id":null,"index":0}`, http.StatusNotFound)
	privatePage := id(request(ownerJWT, "POST", base+"/teams/"+private+"/wiki", `{"title":"Hidden"}`, http.StatusCreated))
	request(memberJWT, "GET", base+"/teams/"+private+"/wiki/tree", "", http.StatusNotFound)
	request(memberJWT, "POST", base+"/teams/"+private+"/wiki/"+privatePage+"/move", `{"parent_id":null,"index":0}`, http.StatusNotFound)
	request(guestJWT, "POST", wiki+"/"+setup+"/move", `{"parent_id":null,"index":1}`, http.StatusForbidden)
	request(memberJWT, "POST", wiki+"/"+setup+"/move", `{"parent_id":"not-a-uuid","index":1}`, http.StatusBadRequest)

	// Deleting a folder keeps its pages, lifted to the folder's parent.
	request(memberJWT, "DELETE", wiki+"/"+folder, "", http.StatusOK)
	nodes = tree(memberJWT)
	require.NotContains(t, nodes, folder)
	require.Equal(t, "", parentOf(nodes[faq]))
	require.Equal(t, "", parentOf(nodes[intro]))
}
