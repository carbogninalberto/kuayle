package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestSCIMProvisioning(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const secret = "scim-http-test-secret-with-enough-length"
	users := repository.NewUserRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	refresh := repository.NewRefreshTokenRepository(db)
	pats := repository.NewPersonalAccessTokenRepository(db)
	auth := service.NewAuthService(users, refresh, secret)
	authH := handler.NewAuthHandler(auth, false, mw.NewLoginThrottle(5, 0), nil)
	e := echo.New()
	m := testMiddleware(mw.AuthWithSessionVersions(secret, pats, nil, refresh))
	m.workspaceMembership = mw.WorkspaceMembership(workspaces, mw.SSONotEnforced)
	tokens := service.NewTokenService(pats, workspaces)
	h := &appHandlers{
		auth:    authH,
		token:   handler.NewTokenHandler(tokens),
		cliLink: handler.NewCLILinkHandler(service.NewCLILinkService(repository.NewCLILinkRepository(db), tokens, "https://kuayle.example.test")),
	}
	h.scim = handler.NewSCIMHandler(service.NewSCIMService(repository.NewSCIMRepository(db), service.NewWorkspaceService(workspaces, users)), "https://kuayle.example.test")
	registerRoutes(e, h, m)

	request := func(method, path, token, body string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if strings.HasPrefix(path, "/api/scim/") {
			req.Header.Set("Content-Type", "application/scim+json")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		if strings.HasPrefix(path, "/api/scim/") && w.Body.Len() > 0 {
			require.Equal(t, "application/scim+json", w.Header().Get("Content-Type"))
		}
		require.Equal(t, status, w.Code, "%s %s", method, path)
		var result map[string]any
		if w.Body.Len() > 0 {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		}
		return result
	}
	jsonBody := func(value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		require.NoError(t, err)
		return string(data)
	}
	owner, wsA, wsB := uuid.New(), uuid.New(), uuid.New()
	slugA, slugB := "scim-"+wsA.String(), "scim-"+wsB.String()
	_, err = db.Exec(`INSERT INTO users (id,email,name,password_hash) VALUES ($1,$2,'Owner','unused')`, owner, owner.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	for _, fixture := range []struct {
		id   uuid.UUID
		slug string
	}{{wsA, slugA}, {wsB, slugB}} {
		_, err = db.Exec(`INSERT INTO workspaces (id,name,slug,owner_id) VALUES ($1,'SCIM Test',$2,$3)`, fixture.id, fixture.slug, owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, fixture.id) })
		_, err = db.Exec(`INSERT INTO workspace_members (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, fixture.id, owner)
		require.NoError(t, err)
	}
	ownerAccess, _, err := auth.IssueSSOSession(context.Background(), owner, wsA)
	require.NoError(t, err)
	ownerB, _, err := auth.IssueSSOSession(context.Background(), owner, wsB)
	require.NoError(t, err)
	adminA := "/api/workspaces/" + slugA + "/scim"
	adminB := "/api/workspaces/" + slugB + "/scim"
	baseA, baseB := "/api/scim/v2/"+slugA, "/api/scim/v2/"+slugB
	firstToken := request("POST", adminA+"/token", ownerAccess, "", 201)["token"].(string)
	tokenA := request("POST", adminA+"/token", ownerAccess, "", 201)["token"].(string)
	tokenB := request("POST", adminB+"/token", ownerB, "", 201)["token"].(string)
	require.Equal(t, true, request("GET", adminA+"/token", ownerAccess, "", 200)["enabled"])
	var hashed bool
	require.NoError(t, db.Get(&hashed, `SELECT token_hash=$2 FROM workspace_scim_configs WHERE workspace_id=$1`, wsA, domain.HashToken(tokenA)))
	require.True(t, hashed)
	request("GET", baseA+"/Users", firstToken, "", 401)
	request("GET", baseB+"/Users", tokenA, "", 401)
	request("GET", baseA+"/Users", ownerAccess, "", 401)
	require.EqualValues(t, 2, request("GET", baseA+"/Schemas", tokenA, "", 200)["totalResults"])
	request("GET", baseA+"/ServiceProviderConfig", tokenA, "", 200)
	request("GET", baseA+"/ResourceTypes/User", tokenA, "", 200)
	request("GET", baseA+"/Schemas/"+service.SCIMUserSchema, tokenA, "", 200)
	email := uuid.NewString() + "@example.test"
	created := request("POST", baseA+"/Users", tokenA, jsonBody(map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:User"}, "userName": email,
	}), 201)
	userID := uuid.MustParse(created["id"].(string))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID) })
	request("GET", baseB+"/Users/"+userID.String(), tokenB, "", 404)
	request("DELETE", baseB+"/Users/"+userID.String(), tokenB, "", 404)
	filter := url.QueryEscape(`userName eq "` + email + `"`)
	require.EqualValues(t, 1, request("GET", baseA+"/Users?filter="+filter, tokenA, "", 200)["totalResults"])
	require.EqualValues(t, 0, request("GET", baseB+"/Users?filter="+filter, tokenB, "", 200)["totalResults"])

	request("POST", baseA+"/Users", tokenA, jsonBody(map[string]any{
		"schemas": []string{service.SCIMUserSchema}, "userName": owner.String() + "@example.test",
	}), 409)
	userPath := baseA + "/Users/" + userID.String()
	patched := request("PATCH", userPath, tokenA,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","value":{"displayName":"New name","name":{"givenName":"New"}}},{"op":"Replace","path":"emails[type eq \"work\"].value","value":"new@example.test"}]}`, 200)
	require.Equal(t, "New name", patched["displayName"])
	account, err := users.GetByID(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, "New name", account.DisplayName)
	require.EqualValues(t, 1, request("GET", baseA+"/Users?count=0&filter="+url.QueryEscape(`emails[type eq "work"].value eq "NEW@example.test"`), tokenA, "", 200)["totalResults"])
	require.Equal(t, "invalidFilter", request("GET", baseA+"/Users?filter="+url.QueryEscape(`userName sw "x"`), tokenA, "", 400)["scimType"])
	request("PUT", userPath, tokenA, jsonBody(map[string]any{
		"schemas": []string{service.SCIMUserSchema}, "userName": email, "externalId": "entra-" + userID.String(),
	}), 200)

	identity := repository.SSOProvisionParams{WorkspaceID: wsB, Issuer: "https://idp.example.test", Provider: "oidc", Subject: "same",
		Email: email, DefaultRole: "member"}
	sso := repository.NewSSORepository(db)
	_, err = sso.ProvisionIdentity(context.Background(), identity)
	require.ErrorIs(t, err, repository.ErrSSOLinkRequired)
	identity.WorkspaceID = wsA
	linked, err := sso.ProvisionIdentity(context.Background(), identity)
	require.NoError(t, err)
	require.Equal(t, userID, linked.ID)
	identity.Subject = "different"
	_, err = sso.ProvisionIdentity(context.Background(), identity)
	require.ErrorIs(t, err, repository.ErrSSOLinkRequired)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES ($1,$2,'member')`, wsB, userID)
	require.NoError(t, err)

	userAccess, userRefresh, err := auth.IssueSSOSession(context.Background(), userID, wsA)
	require.NoError(t, err)
	request("POST", adminA+"/token", userAccess, "", 403)
	request("GET", "/api/auth/me", userAccess, "", 200)
	pat := request("POST", "/api/tokens", userAccess, `{"name":"SCIM regression","scopes":["account:read"],"workspace_slugs":["`+slugA+`"]}`, 201)["token"].(string)
	request("GET", "/api/auth/me", pat, "", 200)
	request("GET", baseA+"/Users", pat, "", 401)
	request("POST", adminA+"/token", pat, "", 403)
	cli := request("POST", "/api/cli-links", userAccess, jsonBody(map[string]any{
		"scopes": []string{"account:read"}, "workspace_slugs": []string{slugA}, "max_uses": 2,
	}), 201)["link"].(string)
	link, err := url.Parse(cli)
	require.NoError(t, err)
	params, err := url.ParseQuery(link.Fragment)
	require.NoError(t, err)
	codeBody := jsonBody(map[string]string{"code": params.Get("code")})
	request("POST", "/api/cli/connect", "", codeBody, 200)

	teamA, teamB := uuid.New(), uuid.New()
	for _, team := range []struct{ id, ws uuid.UUID }{{teamA, wsA}, {teamB, wsB}} {
		_, err = db.Exec(`INSERT INTO teams(id, workspace_id, name, key, is_private) VALUES ($1,$2,'Provisioned','SCIM',true)`, team.id, team.ws)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO team_members(team_id, user_id) VALUES ($1,$2)`, teamA, owner)
	require.NoError(t, err)
	assertTeamMember := func(id uuid.UUID, want bool) {
		t.Helper()
		var present bool
		require.NoError(t, db.Get(&present, `SELECT EXISTS (SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2)`, teamA, id))
		require.Equal(t, want, present)
	}
	groupPayload := func(name string, members ...uuid.UUID) string {
		refs := make([]domain.SCIMGroupMember, 0, len(members))
		for _, id := range members {
			refs = append(refs, domain.SCIMGroupMember{Value: id})
		}
		return jsonBody(map[string]any{"schemas": []string{service.SCIMGroupSchema}, "displayName": name, "members": refs})
	}
	groupID := request("POST", baseA+"/Groups", tokenA, groupPayload("Engineering", userID), 201)["id"].(string)
	groupA := baseA + "/Groups/" + groupID
	groupB := baseB + "/Groups/" + groupID
	mapping := adminA + "/groups/" + groupID + "/team"
	assertTeamMember(userID, false)
	request("GET", groupB, tokenB, "", 404)
	request("PUT", groupA, tokenA, groupPayload("Invalid update", owner), 400)
	group := request("GET", groupA, tokenA, "", 200)
	require.Equal(t, "Engineering", group["displayName"])
	require.Len(t, group["members"], 1)
	request("POST", baseB+"/Groups", tokenB, groupPayload("Foreign", userID), 400)
	request("PUT", mapping, ownerAccess, jsonBody(map[string]any{"team_id": teamB}), 404)
	request("PUT", mapping, ownerAccess, jsonBody(map[string]any{"team_id": teamA}), 204)
	assertTeamMember(userID, true)
	assertTeamMember(owner, true)
	require.NotContains(t, request("GET", groupA+"?excludedAttributes=members", tokenA, "", 200), "members")
	request("PATCH", groupA, tokenA, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"remove","path":"members[value eq \"`+userID.String()+`\"]"}]}`, 200)
	assertTeamMember(userID, false)
	request("PATCH", groupA, tokenA, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"displayName":"Platform","id":"`+groupID+`"}},{"op":"add","path":"members","value":[{"value":"`+userID.String()+`","$ref":null}]}]}`, 200)
	assertTeamMember(userID, true)
	request("PATCH", groupA, tokenA, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"remove","path":"members","value":[{"value":"`+userID.String()+`"}]}]}`, 200)
	assertTeamMember(userID, false)
	request("PUT", groupA, tokenA, groupPayload("Platform", userID), 200)
	assertTeamMember(userID, true)
	request("DELETE", mapping, ownerAccess, "", 204)
	assertTeamMember(userID, false)
	assertTeamMember(owner, true)
	request("PUT", mapping, ownerAccess, jsonBody(map[string]any{"team_id": teamA}), 204)
	assertTeamMember(userID, true)

	disabled := request("PATCH", baseA+"/Users/"+userID.String(), tokenA,
		`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","path":"active","value":"False"}]}`, 200)
	require.Equal(t, false, disabled["active"])
	member, err := workspaces.GetMember(context.Background(), wsA, userID)
	require.NoError(t, err)
	require.Nil(t, member)
	member, err = workspaces.GetMember(context.Background(), wsB, userID)
	require.NoError(t, err)
	require.NotNil(t, member)
	assertTeamMember(userID, false)
	assertTeamMember(owner, true)
	require.NotContains(t, request("GET", groupA, tokenA, "", 200), "members")
	request("GET", "/api/auth/me", userAccess, "", 401)
	request("GET", "/api/auth/me", pat, "", 401)
	gone := request("POST", "/api/cli/connect", "", codeBody, 410)
	require.Equal(t, "LINK_REVOKED", gone["error"].(map[string]any)["code"])
	req := httptest.NewRequest("POST", "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: userRefresh})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	version := int64(0)
	err = refresh.Create(context.Background(), &repository.RefreshToken{
		ID: uuid.New(), UserID: userID, ExpectedVersion: &version,
	})
	require.ErrorIs(t, err, sql.ErrNoRows)
	freshAccess, _, err := auth.IssueSSOSession(context.Background(), userID, wsB)
	require.NoError(t, err)
	request("GET", "/api/auth/me", freshAccess, "", 200)
	request("PATCH", userPath, tokenA, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":true}]}`, 200)
	member, err = workspaces.GetMember(context.Background(), wsA, userID)
	require.NoError(t, err)
	require.NotNil(t, member)
	request("DELETE", userPath, tokenA, "", 204)
	request("GET", userPath, tokenA, "", 404)
	request("DELETE", groupA, tokenA, "", 204)
	request("GET", groupA, tokenA, "", 404)
	request("DELETE", adminA+"/token", ownerAccess, "", 204)
	request("GET", baseA+"/Users", tokenA, "", 401)
	request("GET", baseB+"/Groups", tokenB, "", 200)
}
