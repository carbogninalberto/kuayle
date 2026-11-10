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
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// References to wiki pages over real HTTP and PostgreSQL: pages of other
// teams and projects are suggested and resolved, a private team's page is
// never named to someone outside it (in suggestions, chips, attached
// references, backlinks or a public link), and backlinks list only the
// issues and pages the reader can see.
func TestWikiReferencesHTTP(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, member, guest, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ws, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest, stranger} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Reference tester','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'References',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
	}
	for user, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}
	for user, role := range map[uuid.UUID]string{owner: "owner", stranger: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, other, user, role)
		require.NoError(t, err)
	}

	workspaces := repository.NewWorkspaceRepository(db)
	teams := repository.NewTeamRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	wikiRepo := repository.NewWikiRepository(db)
	wikiSvc := service.NewWikiService(wikiRepo)
	wikiSvc.SetIssues(repository.NewIssueRepository(db))
	handlers := &appHandlers{
		workspace:   handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, repository.NewUserRepository(db), hub)),
		team:        handler.NewTeamHandler(service.NewTeamService(teams, repository.NewTeamStatusRepository(db), hub)),
		project:     handler.NewProjectHandler(service.NewProjectService(repository.NewProjectRepository(db))),
		wiki:        handler.NewWikiHandler(wikiSvc, &fakeWikiAI{}),
		wikiPublish: handler.NewWikiPublishHandler(service.NewWikiPublishService(wikiRepo, repository.NewSharedLinkRepository(db), workspaces, "reference-secret"), "https://kuayle.test"),
	}
	secret := "wiki-references-http-test-secret"
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
	id := func(data []byte) string {
		t.Helper()
		var body struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(data, &body))
		return body.ID
	}
	type reference struct {
		ID         string  `json:"id"`
		Accessible bool    `json:"accessible"`
		Title      string  `json:"title"`
		TeamID     *string `json:"team_id"`
		ProjectID  *string `json:"project_id"`
		ScopeName  string  `json:"scope_name"`
	}
	references := func(data []byte) []reference {
		t.Helper()
		var out []reference
		require.NoError(t, json.Unmarshal(data, &out))
		return out
	}
	titles := func(list []reference) []string {
		out := []string{}
		for _, item := range list {
			out = append(out, item.Title)
		}
		return out
	}
	ownerJWT, memberJWT, guestJWT := jwt(owner), jwt(member), jwt(guest)
	base := "/api/workspaces/" + ws.String()

	docs := id(request(ownerJWT, "POST", base+"/teams", `{"name":"Docs","key":"DOC"}`, http.StatusCreated))
	platform := id(request(ownerJWT, "POST", base+"/teams", `{"name":"Platform","key":"PLT"}`, http.StatusCreated))
	private := id(request(ownerJWT, "POST", base+"/teams", `{"name":"Secret","key":"SEC","is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusCreated))
	project := id(request(ownerJWT, "POST", base+"/projects", `{"name":"Launch","team_id":"`+platform+`"}`, http.StatusCreated))
	docsWiki, projectWiki, privateWiki := base+"/teams/"+docs+"/wiki", base+"/projects/"+project+"/wiki", base+"/teams/"+private+"/wiki"

	guide := id(request(memberJWT, "POST", docsWiki, `{"title":"Deploy guide","content":"<p>How to deploy</p>"}`, http.StatusCreated))
	secretPage := id(request(ownerJWT, "POST", privateWiki, `{"title":"Acquisition plan","content":"<p>classified</p>"}`, http.StatusCreated))
	mention := func(page string) string { return sanitize.WikiMentionHTML(uuid.MustParse(page)) }

	// A mention is stored without any title a client sends with it.
	labelled := `<span data-type=\"mention\" data-kind=\"wiki\" data-id=\"` + secretPage + `\" data-label=\"Acquisition plan\">Acquisition plan</span>`
	launch := id(request(memberJWT, "POST", projectWiki, `{"title":"Launch plan","content":"<p>Read `+strings.ReplaceAll(mention(guide), `"`, `\"`)+` and `+labelled+`</p>"}`, http.StatusCreated))
	var stored struct {
		Content string `json:"content"`
	}
	require.NoError(t, json.Unmarshal(request(memberJWT, "GET", projectWiki+"/"+launch, "", http.StatusOK), &stored))
	require.NotContains(t, stored.Content, "Acquisition")
	require.Contains(t, stored.Content, mention(guide))
	require.Contains(t, stored.Content, mention(secretPage))
	// The private page mentions the guide too.
	request(ownerJWT, "PATCH", privateWiki+"/"+secretPage, `{"version":1,"content":"<p>`+strings.ReplaceAll(mention(guide), `"`, `\"`)+`</p>"}`, http.StatusOK)

	// Suggestions cross teams and projects but stop at what the caller sees.
	suggested := references(request(memberJWT, "GET", base+"/wiki/references?q=PLAN", "", http.StatusOK))
	require.Equal(t, []string{"Launch plan"}, titles(suggested))
	require.Equal(t, "Launch", suggested[0].ScopeName)
	require.Equal(t, project, *suggested[0].ProjectID)
	require.True(t, suggested[0].Accessible)
	require.ElementsMatch(t, []string{"Launch plan", "Acquisition plan"}, titles(references(request(ownerJWT, "GET", base+"/wiki/references?q=plan", "", http.StatusOK))))
	require.ElementsMatch(t, []string{"Launch plan", "Deploy guide"}, titles(references(request(guestJWT, "GET", base+"/wiki/references", "", http.StatusOK))))
	require.Empty(t, references(request(memberJWT, "GET", base+"/wiki/references?q=%25", "", http.StatusOK)), "a wildcard is matched literally")
	require.Len(t, references(request(memberJWT, "GET", base+"/wiki/references?limit=1", "", http.StatusOK)), 1)
	request(jwt(stranger), "GET", base+"/wiki/references", "", http.StatusForbidden)
	require.Empty(t, references(request(jwt(stranger), "GET", "/api/workspaces/"+other.String()+"/wiki/references", "", http.StatusOK)), "another workspace's pages are never suggested")

	// Chips: a visible page resolves to its title and scope; an invisible,
	// foreign or unknown page is the same bare placeholder.
	unknown := uuid.NewString()
	resolveBody := `{"ids":["` + guide + `","` + secretPage + `","` + unknown + `"]}`
	raw := request(memberJWT, "POST", base+"/wiki/references/resolve", resolveBody, http.StatusOK)
	require.NotContains(t, string(raw), "Acquisition")
	require.NotContains(t, string(raw), private)
	resolved := references(raw)
	require.Equal(t, reference{ID: guide, Accessible: true, Title: "Deploy guide", TeamID: &docs, ScopeName: "Docs"}, resolved[0])
	require.Equal(t, []reference{{ID: secretPage}, {ID: unknown}}, resolved[1:])
	require.Equal(t, "Acquisition plan", references(request(ownerJWT, "POST", base+"/wiki/references/resolve", resolveBody, http.StatusOK))[1].Title)
	require.Equal(t, []reference{{ID: guide}}, references(request(jwt(stranger), "POST", "/api/workspaces/"+other.String()+"/wiki/references/resolve", `{"ids":["`+guide+`"]}`, http.StatusOK)))
	request(memberJWT, "POST", base+"/wiki/references/resolve", `{"ids":["nope"]}`, http.StatusBadRequest)

	// Issues: DOC-1 mentions the guide in its description, DOC-2 does not;
	// the private team's SEC-1 mentions it in a comment.
	issue := func(team, identifier, description string) uuid.UUID {
		t.Helper()
		issueID := uuid.New()
		_, err := db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,creator_id,status_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`,
			issueID, ws, team, identifier[len(identifier)-1:], identifier, "Issue "+identifier, description, owner)
		require.NoError(t, err)
		return issueID
	}
	issue(docs, "DOC-1", "<p>Follow "+mention(guide)+"</p>")
	issue(docs, "DOC-2", "<p>Nothing here</p>")
	hiddenIssue := issue(private, "SEC-1", "<p>Private work</p>")
	_, err = db.Exec(`INSERT INTO comments(issue_id,user_id,body) VALUES($1,$2,$3)`, hiddenIssue, owner, "<p>"+mention(guide)+"</p>")
	require.NoError(t, err)

	// Attaching a page to an issue needs an issue edit and a visible page.
	attach := base + "/issues/DOC-2/wiki-references"
	request(guestJWT, "POST", attach, `{"page_id":"`+guide+`"}`, http.StatusForbidden)
	request(memberJWT, "POST", attach, `{"page_id":"`+secretPage+`"}`, http.StatusNotFound)
	request(memberJWT, "POST", base+"/issues/SEC-1/wiki-references", `{"page_id":"`+guide+`"}`, http.StatusNotFound)
	request(memberJWT, "POST", attach, `{"page_id":"nope"}`, http.StatusBadRequest)
	require.Empty(t, references(request(memberJWT, "GET", attach, "", http.StatusOK)))
	require.Equal(t, []string{"Deploy guide"}, titles(references(request(memberJWT, "POST", attach, `{"page_id":"`+guide+`"}`, http.StatusOK))))
	request(memberJWT, "POST", attach, `{"page_id":"`+guide+`"}`, http.StatusOK)
	// The owner attaches the private page: others see a placeholder for it.
	request(ownerJWT, "POST", attach, `{"page_id":"`+secretPage+`"}`, http.StatusOK)
	raw = request(guestJWT, "GET", attach, "", http.StatusOK)
	require.NotContains(t, string(raw), "Acquisition")
	require.Equal(t, []reference{{ID: guide, Accessible: true, Title: "Deploy guide", TeamID: &docs, ScopeName: "Docs"}, {ID: secretPage}}, references(raw))
	request(memberJWT, "GET", base+"/issues/SEC-1/wiki-references", "", http.StatusNotFound)
	// A token limited to issues cannot read page titles through an issue.
	pat, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: member, Name: "Issues only", TokenHash: hash, TokenPrefix: prefix, Scopes: pq.StringArray{"issues:read"}}))
	request(pat, "GET", attach, "", http.StatusForbidden)

	// Backlinks of the guide: the member sees the public issues and page,
	// the owner also the private team's issue and page.
	type backlinks struct {
		Issues []struct {
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
		} `json:"issues"`
		Pages []reference `json:"pages"`
	}
	linked := func(token string) (issues, pages []string) {
		t.Helper()
		var links backlinks
		require.NoError(t, json.Unmarshal(request(token, "GET", docsWiki+"/"+guide+"/backlinks", "", http.StatusOK), &links))
		for _, item := range links.Issues {
			issues = append(issues, item.Identifier)
		}
		return issues, titles(links.Pages)
	}
	issues, pages := linked(memberJWT)
	require.ElementsMatch(t, []string{"DOC-1", "DOC-2"}, issues)
	require.Equal(t, []string{"Launch plan"}, pages)
	issues, pages = linked(ownerJWT)
	require.ElementsMatch(t, []string{"DOC-1", "DOC-2", "SEC-1"}, issues)
	require.ElementsMatch(t, []string{"Launch plan", "Acquisition plan"}, pages)
	request(memberJWT, "GET", privateWiki+"/"+secretPage+"/backlinks", "", http.StatusNotFound)
	request(memberJWT, "GET", projectWiki+"/"+guide+"/backlinks", "", http.StatusNotFound)

	// Anyone who may edit the issue can remove a reference they cannot open.
	request(guestJWT, "DELETE", attach+"/"+secretPage, "", http.StatusForbidden)
	request(memberJWT, "DELETE", attach+"/"+secretPage, "", http.StatusOK)
	require.Equal(t, []string{"Deploy guide"}, titles(references(request(ownerJWT, "GET", attach, "", http.StatusOK))))
	request(memberJWT, "DELETE", attach+"/"+guide, "", http.StatusOK)
	issues, _ = linked(memberJWT)
	require.Equal(t, []string{"DOC-1"}, issues)

	// A public link names a mentioned page only when an anonymous reader
	// may know it: the unpublished project page by title (the reader prints
	// it as plain text), the private team's page not at all.
	var state struct {
		URL *string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(request(ownerJWT, "PUT", projectWiki+"/"+launch+"/publish", `{"mode":"public"}`, http.StatusOK), &state))
	require.NotNil(t, state.URL)
	resp, err := http.Get(server.URL + "/api/public/wiki/" + (*state.URL)[strings.LastIndex(*state.URL, "/")+1:])
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	public, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(public))
	require.NotContains(t, string(public), "Acquisition")
	var shown struct {
		Pages []struct {
			Content string `json:"content"`
		} `json:"pages"`
		References []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"references"`
	}
	require.NoError(t, json.Unmarshal(public, &shown))
	require.Len(t, shown.References, 1)
	require.Equal(t, guide, shown.References[0].ID)
	require.Equal(t, "Deploy guide", shown.References[0].Title)
	require.Contains(t, shown.Pages[0].Content, mention(secretPage))

	// Deleting a page removes it from the issues it was attached to.
	request(memberJWT, "POST", attach, `{"page_id":"`+guide+`"}`, http.StatusOK)
	request(memberJWT, "DELETE", docsWiki+"/"+guide, "", http.StatusOK)
	require.Empty(t, references(request(memberJWT, "GET", attach, "", http.StatusOK)))
}
