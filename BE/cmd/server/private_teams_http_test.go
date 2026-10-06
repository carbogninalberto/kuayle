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
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Real TCP HTTP requests through the production route manifest, JWT/PAT auth,
// live workspace/role middleware and PostgreSQL repositories. No auth mocks.
func TestPrivateTeamsHTTPAndPAT(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, admin, member, guest, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ws, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'HTTP privacy','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'HTTP privacy',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
		for user, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
			_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, id, user, role)
			require.NoError(t, err)
		}
	}
	users := repository.NewUserRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	teams := repository.NewTeamRepository(db)
	statuses := repository.NewTeamStatusRepository(db)
	issues := repository.NewIssueRepository(db)
	projects := repository.NewProjectRepository(db)
	cycles := repository.NewCycleRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	notifications := service.NewNotificationService(repository.NewNotificationRepository(db))
	teamService := service.NewTeamService(teams, statuses, hub)
	issueService := service.NewIssueService(issues, teams, statuses, repository.NewIssueHistoryRepository(db), hub, notifications, projects)
	commentService := service.NewCommentService(repository.NewCommentRepository(db), issues, hub, notifications)
	relationService := service.NewIssueRelationService(repository.NewIssueRelationRepository(db), issues)
	handlers := &appHandlers{workspace: handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, users, hub)), team: handler.NewTeamHandler(teamService), issue: handler.NewIssueHandler(issueService, commentService, users, statuses, projects, cycles, relationService)}
	secret := "private-team-http-test-secret"
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
	ownerJWT, adminJWT, memberJWT, guestJWT := jwt(owner), jwt(admin), jwt(member), jwt(guest)
	pat := func(user uuid.UUID, scopes []string, slugs []string) string {
		t.Helper()
		raw, hash, prefix, err := domain.GenerateToken()
		require.NoError(t, err)
		require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "HTTP privacy", TokenHash: hash, TokenPrefix: prefix, Scopes: pq.StringArray(scopes), WorkspaceSlugs: pq.StringArray(slugs)}))
		return raw
	}
	memberPAT := pat(member, []string{"issues:read", "teams:read"}, []string{ws.String()})
	adminReadPAT := pat(admin, []string{"issues:read", "teams:read"}, nil)
	adminManagePAT := pat(admin, []string{"team:manage", "teams:read", "members:read"}, []string{ws.String()})
	memberManagePAT := pat(member, []string{"team:manage", "issue:create"}, nil)
	guestWritePAT := pat(guest, []string{"issue:create", "issue:update", "issues:read"}, nil)
	request := func(token, method, path, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(token, domain.TokenPrefix) {
			req.Header.Set("Authorization", "Bearer "+token)
		} else if token != "" {
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
	base := "/api/workspaces/" + ws.String()
	privateBody := `{"name":"Restricted team","key":"PRIV","is_private":true}`
	request(ownerJWT, "POST", base+"/teams", privateBody, http.StatusBadRequest)
	request(memberManagePAT, "POST", base+"/teams", privateBody, http.StatusForbidden)
	created := request(ownerJWT, "POST", base+"/teams", `{"name":"Restricted team","key":"PRIV","is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusCreated)
	var envelope struct {
		ID        string `json:"id"`
		IsPrivate bool   `json:"is_private"`
	}
	require.NoError(t, json.Unmarshal(created, &envelope))
	require.True(t, envelope.IsPrivate)
	teamID := uuid.MustParse(envelope.ID)
	teamPath := base + "/teams/" + teamID.String()
	var count int
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM team_statuses WHERE team_id=$1`, teamID))
	require.Equal(t, 6, count)
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM team_members WHERE team_id=$1`, teamID))
	require.Equal(t, 1, count)
	request(adminJWT, "GET", teamPath, "", http.StatusOK)
	request(memberJWT, "GET", teamPath, "", http.StatusNotFound)
	request(memberPAT, "GET", teamPath, "", http.StatusNotFound)
	require.NotContains(t, string(request(memberJWT, "GET", base+"/teams", "", http.StatusOK)), "Restricted team")
	request(ownerJWT, "POST", teamPath+"/members", `{"user_id":"`+outsider.String()+`"}`, http.StatusNotFound)
	request(adminReadPAT, "POST", teamPath+"/members", `{"user_id":"`+member.String()+`"}`, http.StatusForbidden)
	request(adminManagePAT, "POST", teamPath+"/members", `{"user_id":"`+member.String()+`"}`, http.StatusOK)
	request(ownerJWT, "POST", teamPath+"/members", `{"user_id":"`+guest.String()+`"}`, http.StatusOK)
	request(memberJWT, "GET", teamPath, "", http.StatusOK)
	request(memberPAT, "GET", teamPath, "", http.StatusOK)
	request(memberPAT, "GET", teamPath+"/members", "", http.StatusForbidden)
	request(adminManagePAT, "GET", teamPath+"/members", "", http.StatusOK)
	issueBody := `{"team_id":"` + teamID.String() + `","title":"Confidential HTTP issue"}`
	request(ownerJWT, "POST", base+"/issues", issueBody, http.StatusCreated)
	issuePath := base + "/issues/PRIV-1"
	request(memberPAT, "GET", issuePath, "", http.StatusOK)
	request(guestJWT, "GET", issuePath, "", http.StatusOK)
	request(memberPAT, "POST", base+"/issues", issueBody, http.StatusForbidden)
	request(guestWritePAT, "POST", base+"/issues", issueBody, http.StatusForbidden)
	request(guestWritePAT, "PATCH", issuePath, `{"title":"forbidden"}`, http.StatusForbidden)
	request(adminReadPAT, "PATCH", issuePath, `{"title":"forbidden"}`, http.StatusForbidden)
	request(ownerJWT, "DELETE", teamPath+"/members/"+member.String(), "", http.StatusOK)
	request(memberJWT, "GET", issuePath, "", http.StatusNotFound)
	request(memberPAT, "GET", issuePath, "", http.StatusNotFound)
	require.NotContains(t, string(request(memberPAT, "GET", base+"/issues?search=Confidential", "", http.StatusOK)), "Confidential HTTP issue")
	request(adminReadPAT, "GET", issuePath, "", http.StatusOK)
	// Administrative access is live: a demoted administrator without explicit
	// team membership loses access through both old session and PAT credentials.
	request(ownerJWT, "PATCH", base+"/members/"+admin.String(), `{"role":"member"}`, http.StatusOK)
	request(adminJWT, "GET", issuePath, "", http.StatusNotFound)
	request(adminReadPAT, "GET", issuePath, "", http.StatusNotFound)
	request(ownerJWT, "PATCH", base+"/members/"+admin.String(), `{"role":"admin"}`, http.StatusOK)
	request(adminReadPAT, "GET", issuePath, "", http.StatusOK)
	request(ownerJWT, "PATCH", teamPath+"/visibility", `{"is_private":false}`, http.StatusBadRequest)
	request(memberPAT, "GET", issuePath, "", http.StatusNotFound)
	request(adminManagePAT, "PATCH", teamPath+"/visibility", `{"is_private":false,"confirm_public":true}`, http.StatusOK)
	request(memberPAT, "GET", issuePath, "", http.StatusOK)
	request(ownerJWT, "PATCH", teamPath+"/visibility", `{"is_private":true}`, http.StatusBadRequest)
	request(ownerJWT, "PATCH", teamPath+"/visibility", `{"is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusOK)
	request(memberPAT, "GET", issuePath, "", http.StatusNotFound)
	otherPath := "/api/workspaces/" + other.String() + "/teams/" + teamID.String()
	request(adminJWT, "GET", otherPath, "", http.StatusNotFound)
	request(adminManagePAT, "GET", otherPath, "", http.StatusForbidden)
	request(jwt(outsider), "GET", teamPath, "", http.StatusForbidden)
	// A public team still requires no explicit membership.
	public := request(ownerJWT, "POST", base+"/teams", `{"name":"Public team","key":"PUB"}`, http.StatusCreated)
	require.NoError(t, json.Unmarshal(public, &envelope))
	require.False(t, envelope.IsPrivate)
	request(memberJWT, "GET", base+"/teams/"+envelope.ID, "", http.StatusOK)
	// Failed status initialization rolls back team, membership and privacy marker.
	broken := &domain.Team{ID: uuid.New(), WorkspaceID: other, Name: "Rollback", Key: "ROLL", IsPrivate: true}
	duplicate := domain.TeamStatus{ID: uuid.New(), Name: "Todo", Slug: "todo", Category: domain.StatusCategoryUnstarted}
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: other})
	require.Error(t, teams.CreateWithDefaults(ctx, broken, owner, []domain.TeamStatus{duplicate, duplicate}))
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM teams WHERE id=$1`, broken.ID))
	require.Zero(t, count)
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM workspace_privacy WHERE workspace_id=$1`, other))
	require.Zero(t, count)
	// Reuse existing JWT/PAT after a workspace-level revocation.
	request(ownerJWT, "DELETE", base+"/members/"+guest.String(), "", http.StatusOK)
	request(guestJWT, "GET", issuePath, "", http.StatusForbidden)
	request(guestWritePAT, "GET", issuePath, "", http.StatusForbidden)
}
