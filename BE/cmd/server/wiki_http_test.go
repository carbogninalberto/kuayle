package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
	ai := &fakeWikiAI{}
	wikiRepo := repository.NewWikiRepository(db)
	wikiSvc := service.NewWikiService(wikiRepo)
	wikiSvc.SetOverviewAI(ai)
	handlers := &appHandlers{
		workspace: handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, users, hub)),
		team:      handler.NewTeamHandler(service.NewTeamService(teams, repository.NewTeamStatusRepository(db), hub)),
		project:   handler.NewProjectHandler(service.NewProjectService(repository.NewProjectRepository(db))),
		wiki:      handler.NewWikiHandler(wikiSvc, ai),
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

	// Each save is recorded as a revision; saves by the same person in quick
	// succession (autosave) extend one revision instead of adding rows.
	type revision struct {
		Version      int     `json:"version"`
		OldTitle     string  `json:"old_title"`
		NewTitle     string  `json:"new_title"`
		OldContent   string  `json:"old_content"`
		NewContent   string  `json:"new_content"`
		EditedByName *string `json:"edited_by_name"`
	}
	var revisions []revision
	decode(request(guestJWT, "GET", pagePath+"/versions", "", http.StatusOK), &revisions)
	require.Len(t, revisions, 1)
	require.Equal(t, revision{Version: 2, OldTitle: "Releases", NewTitle: "Releases", OldContent: `<h2>Release checklist</h2><ul><li><p>Tag</p></li></ul>`, NewContent: "<p>Second draft</p>", EditedByName: revisions[0].EditedByName}, revisions[0])
	request(ownerJWT, "PATCH", pagePath, `{"title":"Release guide","version":2}`, http.StatusOK)
	decode(request(guestJWT, "GET", pagePath+"/versions", "", http.StatusOK), &revisions)
	require.Len(t, revisions, 1, "the owner's quick follow-up joins their revision")
	require.Equal(t, 3, revisions[0].Version)
	require.Equal(t, "Releases", revisions[0].OldTitle)
	require.Equal(t, "Release guide", revisions[0].NewTitle)
	require.Equal(t, "<p>Second draft</p>", revisions[0].NewContent)
	request(memberJWT, "PATCH", pagePath, `{"title":"Release guide","version":3}`, http.StatusOK)
	decode(request(guestJWT, "GET", pagePath+"/versions", "", http.StatusOK), &revisions)
	require.Len(t, revisions, 1, "a save that changes nothing records nothing")
	request(memberJWT, "PATCH", pagePath, `{"title":"Releases","version":4}`, http.StatusOK)
	decode(request(guestJWT, "GET", pagePath+"/versions", "", http.StatusOK), &revisions)
	require.Len(t, revisions, 2, "another editor starts a new revision")
	require.Equal(t, revision{Version: 5, OldTitle: "Release guide", NewTitle: "Releases", OldContent: "<p>Second draft</p>", NewContent: "<p>Second draft</p>", EditedByName: revisions[0].EditedByName}, revisions[0])
	require.Equal(t, 3, revisions[1].Version)

	// AI rework reads the stored page; guests cannot write, so they cannot ask.
	request(guestJWT, "POST", pagePath+"/rework", `{"selected_text":"Second draft"}`, http.StatusForbidden)
	request(memberJWT, "POST", pagePath+"/rework", `{"selected_text":""}`, http.StatusBadRequest)
	var reworked struct {
		Description string `json:"description"`
	}
	decode(request(memberJWT, "POST", pagePath+"/rework", `{"selected_text":"Second draft"}`, http.StatusOK), &reworked)
	require.Equal(t, "<p>Reworked</p>", reworked.Description)
	require.Equal(t, []string{"Releases", "<p>Second draft</p>", "Second draft"}, ai.calls[0])

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
		request(memberJWT, "GET", path+"/versions", "", http.StatusNotFound)
		request(memberJWT, "POST", path+"/rework", `{"selected_text":"classified"}`, http.StatusNotFound)
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
	request(ownerJWT, "GET", privateWiki+"/"+createdPage.ID+"/versions", "", http.StatusNotFound)
	require.Len(t, ai.calls, 1, "denied rework requests never reach the model")
	request(ownerJWT, "GET", base+"/projects/"+publicTeam+"/wiki", "", http.StatusNotFound)
	decode(request(ownerJWT, "GET", projectWiki, "", http.StatusOK), &listed)
	require.Len(t, listed, 1)
	require.Equal(t, "Project notes", listed[0].Title)

	// The registry is opt-in per wiki; writers turn it on, and entries land on
	// one page per UTC day, also under concurrent appends.
	registryOn := func(token, path string) bool {
		t.Helper()
		var settings struct {
			RegistryEnabled  bool   `json:"registry_enabled"`
			OverviewTimezone string `json:"overview_timezone"`
		}
		decode(request(token, "GET", path+"/settings", "", http.StatusOK), &settings)
		require.Equal(t, "UTC", settings.OverviewTimezone)
		return settings.RegistryEnabled
	}
	require.False(t, registryOn(guestJWT, projectWiki))
	request(memberJWT, "POST", projectWiki+"/registry", `{"text":"KUA-20 implemented"}`, http.StatusConflict)
	request(guestJWT, "PATCH", projectWiki+"/settings", `{"registry_enabled":true}`, http.StatusForbidden)
	request(memberJWT, "PATCH", projectWiki+"/settings", `{}`, http.StatusBadRequest)
	request(memberJWT, "PATCH", projectWiki+"/settings", `{"registry_enabled":true}`, http.StatusOK)
	require.True(t, registryOn(guestJWT, projectWiki))
	require.False(t, registryOn(memberJWT, teamWiki), "settings are per wiki")
	request(guestJWT, "POST", projectWiki+"/registry", `{"text":"guest"}`, http.StatusForbidden)
	request(memberJWT, "POST", projectWiki+"/registry", `{"text":"   "}`, http.StatusBadRequest)
	var registryPage struct {
		page
		RegistryDate *string `json:"registry_date"`
	}
	decode(request(memberJWT, "POST", projectWiki+"/registry", `{"text":"KUA-20 implemented"}`, http.StatusOK), &registryPage)
	today := time.Now().UTC().Format(time.DateOnly)
	require.Equal(t, "Registry "+today, registryPage.Title)
	require.Equal(t, today, *registryPage.RegistryDate)
	require.Contains(t, registryPage.Content, " KUA-20 implemented</p>")
	var appends sync.WaitGroup
	for i := 0; i < 8; i++ {
		appends.Add(1)
		go func() {
			defer appends.Done()
			request(ownerJWT, "POST", projectWiki+"/registry", `{"text":"parallel entry"}`, http.StatusOK)
		}()
	}
	appends.Wait()
	decode(request(guestJWT, "GET", projectWiki+"/"+registryPage.ID, "", http.StatusOK), &registryPage)
	require.Equal(t, 9, registryPage.Version)
	require.Equal(t, 8, strings.Count(registryPage.Content, "parallel entry"))
	var registryList []struct {
		Title        string  `json:"title"`
		RegistryDate *string `json:"registry_date"`
	}
	decode(request(guestJWT, "GET", projectWiki, "", http.StatusOK), &registryList)
	require.Len(t, registryList, 2, "one registry page for the day, next to the project notes")
	decode(request(guestJWT, "GET", projectWiki+"/"+registryPage.ID+"/versions", "", http.StatusOK), &revisions)
	require.NotEmpty(t, revisions, "appends are recorded as versions")
	request(ownerJWT, "PATCH", privateProjectWiki+"/settings", `{"registry_enabled":true}`, http.StatusOK)
	request(memberJWT, "GET", privateProjectWiki+"/settings", "", http.StatusNotFound)
	request(memberJWT, "PATCH", privateProjectWiki+"/settings", `{"registry_enabled":false}`, http.StatusNotFound)
	request(memberJWT, "POST", privateProjectWiki+"/registry", `{"text":"planted"}`, http.StatusNotFound)
	request(memberJWT, "PATCH", projectWiki+"/settings", `{"registry_enabled":false}`, http.StatusOK)
	request(memberJWT, "POST", projectWiki+"/registry", `{"text":"after off"}`, http.StatusConflict)

	// The registry feed pages through days, newest first, even while off.
	for _, day := range []string{"2026-01-02", "2026-01-01"} {
		_, err = db.Exec(`INSERT INTO wiki_pages(workspace_id,project_id,title,content,registry_date) VALUES($1,$2,$3,'<p><strong>09:00</strong> old</p>',$4)`, ws, publicProject, "Registry "+day, day)
		require.NoError(t, err)
	}
	type feed struct {
		Days []struct {
			Date    string `json:"date"`
			PageID  string `json:"page_id"`
			Title   string `json:"title"`
			Content string `json:"content"`
			Version int    `json:"version"`
		} `json:"days"`
		NextBefore *string `json:"next_before"`
	}
	var registryFeed feed
	decode(request(guestJWT, "GET", projectWiki+"/registry?limit=2", "", http.StatusOK), &registryFeed)
	require.Len(t, registryFeed.Days, 2)
	require.Equal(t, today, registryFeed.Days[0].Date)
	require.Equal(t, registryPage.ID, registryFeed.Days[0].PageID)
	require.Equal(t, "2026-01-02", registryFeed.Days[1].Date)
	require.Equal(t, "2026-01-02", *registryFeed.NextBefore)
	decode(request(guestJWT, "GET", projectWiki+"/registry?before="+*registryFeed.NextBefore, "", http.StatusOK), &registryFeed)
	require.Len(t, registryFeed.Days, 1)
	require.Equal(t, "<p><strong>09:00</strong> old</p>", registryFeed.Days[0].Content)
	require.Nil(t, registryFeed.NextBefore)
	decode(request(guestJWT, "GET", teamWiki+"/registry", "", http.StatusOK), &registryFeed)
	require.Empty(t, registryFeed.Days, "other wikis' registries stay apart")
	request(guestJWT, "GET", projectWiki+"/registry?limit=32", "", http.StatusBadRequest)
	request(guestJWT, "GET", projectWiki+"/registry?before=yesterday", "", http.StatusBadRequest)
	request(memberJWT, "GET", privateProjectWiki+"/registry", "", http.StatusNotFound)

	// AI overviews: writers generate them, readers list them; the schedule
	// is validated and claimed once per slot.
	request(guestJWT, "POST", projectWiki+"/registry/overviews", `{"period_start":"2026-01-01","period_end":"2026-01-02"}`, http.StatusForbidden)
	request(memberJWT, "POST", projectWiki+"/registry/overviews", `{"period_start":"2026-01-01","period_end":"2026-03-01"}`, http.StatusBadRequest)
	require.Contains(t, string(request(memberJWT, "POST", projectWiki+"/registry/overviews", `{"period_start":"2025-01-01","period_end":"2025-01-02"}`, http.StatusBadRequest)), "NO_REGISTRY_ENTRIES")
	var overview struct {
		ID              string  `json:"id"`
		PeriodStart     string  `json:"period_start"`
		PeriodEnd       string  `json:"period_end"`
		Content         string  `json:"content"`
		GeneratedByName *string `json:"generated_by_name"`
		Trigger         string  `json:"trigger"`
	}
	decode(request(memberJWT, "POST", projectWiki+"/registry/overviews", `{"period_start":"2026-01-01","period_end":"2026-01-02"}`, http.StatusCreated), &overview)
	require.Equal(t, "<h3>Shipped</h3><ul><li>old work</li></ul>", overview.Content)
	require.Equal(t, "manual", overview.Trigger)
	require.Equal(t, "2026-01-01", overview.PeriodStart)
	require.Equal(t, "Wiki tester", *overview.GeneratedByName)
	require.Contains(t, ai.summaries[0], "2026-01-01:\n- 09:00 old")
	request(memberJWT, "POST", projectWiki+"/registry/overviews", `{"period_start":"2026-01-02","period_end":"2026-01-02"}`, http.StatusCreated)
	var overviews struct {
		Overviews []struct {
			ID string `json:"id"`
		} `json:"overviews"`
		NextBefore *string `json:"next_before"`
	}
	decode(request(guestJWT, "GET", projectWiki+"/registry/overviews?limit=1", "", http.StatusOK), &overviews)
	require.Len(t, overviews.Overviews, 1)
	require.NotEqual(t, overview.ID, overviews.Overviews[0].ID, "newest first")
	decode(request(guestJWT, "GET", projectWiki+"/registry/overviews?before="+*overviews.NextBefore, "", http.StatusOK), &overviews)
	require.Equal(t, overview.ID, overviews.Overviews[0].ID)
	require.Nil(t, overviews.NextBefore)
	request(memberJWT, "GET", privateProjectWiki+"/registry/overviews", "", http.StatusNotFound)
	request(memberJWT, "POST", privateProjectWiki+"/registry/overviews", `{"period_start":"2026-01-01","period_end":"2026-01-02"}`, http.StatusNotFound)

	request(memberJWT, "PATCH", projectWiki+"/settings", `{"overview_schedule":{"enabled":true,"frequency":"monthly","weekday":1,"time":"09:00","period_days":7}}`, http.StatusBadRequest)
	request(guestJWT, "PATCH", projectWiki+"/settings", `{"overview_schedule":{"enabled":true,"frequency":"weekly","weekday":1,"time":"09:00","period_days":7}}`, http.StatusForbidden)
	var scheduled struct {
		Schedule struct {
			Enabled    bool   `json:"enabled"`
			Frequency  string `json:"frequency"`
			Weekday    int    `json:"weekday"`
			Time       string `json:"time"`
			PeriodDays int    `json:"period_days"`
		} `json:"overview_schedule"`
		NextRunAt *time.Time `json:"overview_next_run_at"`
	}
	decode(request(memberJWT, "PATCH", projectWiki+"/settings", `{"overview_schedule":{"enabled":true,"frequency":"weekly","weekday":5,"time":"17:30","period_days":7}}`, http.StatusOK), &scheduled)
	require.True(t, scheduled.Schedule.Enabled)
	require.Equal(t, time.Friday, scheduled.NextRunAt.UTC().Weekday())
	require.Equal(t, "17:30", scheduled.NextRunAt.UTC().Format("15:04"))
	require.True(t, registryOn(guestJWT, projectWiki) == false, "the registry switch is unchanged by a schedule save")

	// Two replicas claiming the same due slot: exactly one wins.
	var settingsID uuid.UUID
	require.NoError(t, db.Get(&settingsID, `SELECT id FROM wiki_settings WHERE project_id=$1`, publicProject))
	slot := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	_, err = db.Exec(`UPDATE wiki_settings SET overview_next_run_at=$2 WHERE id=$1`, settingsID, slot)
	require.NoError(t, err)
	var wins sync.WaitGroup
	won := make(chan bool, 2)
	for range 2 {
		wins.Add(1)
		go func() {
			defer wins.Done()
			ok, err := wikiRepo.ClaimOverviewRun(context.Background(), settingsID, slot, slot.Add(7*24*time.Hour))
			require.NoError(t, err)
			won <- ok
		}()
	}
	wins.Wait()
	close(won)
	winners := 0
	for ok := range won {
		if ok {
			winners++
		}
	}
	require.Equal(t, 1, winners)

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

// fakeWikiAI records rework and overview requests instead of calling a model.
type fakeWikiAI struct {
	calls     [][]string
	summaries []string
}

func (f *fakeWikiAI) SummarizeRegistry(_ context.Context, _ uuid.UUID, _, _, _, entries string) (string, error) {
	f.summaries = append(f.summaries, entries)
	return "<h3>Shipped</h3><ul><li>old work</li></ul>", nil
}

func (f *fakeWikiAI) ReworkWikiSelection(_ context.Context, _ uuid.UUID, title, content, selected string) (string, error) {
	f.calls = append(f.calls, []string{title, content, selected})
	return "<p>Reworked</p>", nil
}
