package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/assettoken"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// Real SQL authorization and HTTP handler byte delivery. Authentication is
// supplied as request context here; full JWT/PAT route checks are separate.
func TestAssetPrivacyPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, member, workspace, team, otherTeam, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{owner, member} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Asset privacy','hash')`, user, user.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Asset privacy',$2,$3)`, workspace, workspace.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	for user, role := range map[uuid.UUID]string{owner: "owner", member: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, workspace, user, role)
		require.NoError(t, err)
	}
	for i, id := range []uuid.UUID{team, otherTeam} {
		_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Asset team',$3)`, id, workspace, fmt.Sprintf("AS%d", i))
		require.NoError(t, err)
	}
	status := uuid.New()
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'AS0-1','Asset issue',$4,$5)`, issue, workspace, team, owner, status)
	require.NoError(t, err)
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: workspace})
	memberCtx := domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: workspace})
	assets := repository.NewAssetRepository(db)
	create := func(ctx context.Context, scope *uuid.UUID) *domain.Asset {
		t.Helper()
		a := &domain.Asset{ID: uuid.New(), WorkspaceID: workspace, TeamID: scope, StorageKey: uuid.NewString(), Filename: "private.txt", ContentType: "text/plain", UploadedBy: owner, Size: 12}
		require.NoError(t, assets.Create(ctx, a))
		return a
	}
	owned := create(ownerCtx, &team)
	legacy := create(ownerCtx, nil)
	public := create(ownerCtx, &otherTeam)
	_, err = db.Exec(`UPDATE issues SET description=$1 WHERE id=$2`, `<img src="/api/workspaces/test/assets/`+owned.ID.String()+`">`, issue)
	require.NoError(t, err)
	store := &memoryStorage{files: map[string]string{owned.StorageKey: "secret bytes"}}
	h := NewUploadHandler(store, assets, repository.NewIssueRepository(db), "asset-privacy-test-secret")
	token, _, err := assettoken.Generate("asset-privacy-test-secret:prompt-assets", owned.ID, workspace, issue, time.Hour)
	require.NoError(t, err)
	get := func(ctx context.Context, public bool) *httptest.ResponseRecorder {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/asset", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		if public {
			c.SetParamNames("token")
			c.SetParamValues(token)
			require.NoError(t, h.PublicAsset(c))
		} else {
			c.Set("workspace", &domain.Workspace{ID: workspace})
			c.SetParamNames("assetId")
			c.SetParamValues(owned.ID.String())
			require.NoError(t, h.GetAsset(c))
		}
		return rec
	}
	require.Equal(t, http.StatusOK, get(memberCtx, false).Code)
	require.Equal(t, http.StatusOK, get(context.Background(), true).Code)
	_, err = db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
	require.NoError(t, err)
	denied := get(memberCtx, false)
	require.Equal(t, http.StatusNotFound, denied.Code)
	require.NotContains(t, denied.Body.String(), "secret bytes")
	require.Equal(t, http.StatusNotFound, get(ownerCtx, true).Code, "even an authenticated admin cannot use the old anonymous URL")
	allowed := get(ownerCtx, false)
	require.Equal(t, http.StatusOK, allowed.Code)
	require.Equal(t, "secret bytes", allowed.Body.String())
	require.Equal(t, "private, no-store", allowed.Header().Get("Cache-Control"))
	// Authorized readers cannot mint an anonymous URL for a private issue.
	signReq := httptest.NewRequest(http.MethodPost, "/sign", nil).WithContext(ownerCtx)
	signRec := httptest.NewRecorder()
	signCtx := echo.New().NewContext(signReq, signRec)
	signCtx.Set("workspace", &domain.Workspace{ID: workspace})
	signCtx.SetParamNames("identifier")
	signCtx.SetParamValues("AS0-1")
	require.NoError(t, h.SignIssuePromptAssets(signCtx))
	require.Equal(t, http.StatusForbidden, signRec.Code)
	require.NotContains(t, signRec.Body.String(), "/api/public/assets/")
	// Unknown legacy ownership fails closed; public team-owned assets still work.
	got, err := assets.GetByID(ownerCtx, legacy.ID)
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = assets.GetByID(memberCtx, public.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	deniedUpload := &domain.Asset{ID: uuid.New(), WorkspaceID: workspace, TeamID: &team, StorageKey: uuid.NewString(), Filename: "x", ContentType: "text/plain", UploadedBy: member}
	require.ErrorIs(t, assets.Create(memberCtx, deniedUpload), sql.ErrNoRows)
	deniedUpload.TeamID = nil
	require.ErrorIs(t, assets.Create(ownerCtx, deniedUpload), sql.ErrNoRows)
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, member)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, get(memberCtx, false).Code)
	_, err = db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, member)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, get(memberCtx, false).Code)
	// A private description/comment/history cannot point to a public asset.
	for _, statement := range []string{
		`UPDATE issues SET description=$1 WHERE id=$2`,
		`INSERT INTO comments(issue_id,user_id,body) SELECT $2,creator_id,$1 FROM issues WHERE id=$2`,
		`INSERT INTO issue_history(issue_id,user_id,field,new_value) SELECT $2,creator_id,'description',$1 FROM issues WHERE id=$2`,
	} {
		_, err = db.Exec(statement, "/api/workspaces/test/assets/"+public.ID.String(), issue)
		require.ErrorContains(t, err, "crosses a private-team boundary")
	}
	_, err = db.Exec(`UPDATE assets SET team_id=$1 WHERE id=$2`, otherTeam, owned.ID)
	require.ErrorContains(t, err, "ownership cannot be changed")
	missing := uuid.New()
	_, err = db.Exec(`UPDATE teams SET description=$1 WHERE id=$2`, "/api/workspaces/test/assets/"+missing.String(), otherTeam)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO assets(id,workspace_id,team_id,storage_key,filename,content_type,size,uploaded_by) VALUES($1,$2,$3,$4,'x','text/plain',1,$5)`, missing, workspace, team, missing.String(), owner)
	require.ErrorContains(t, err, "crosses a private-team boundary", "late asset insertion must validate pre-existing references")
	_, err = db.Exec(`UPDATE teams SET is_private=FALSE WHERE id=$1`, team)
	require.NoError(t, err)
	got, err = assets.GetByID(ownerCtx, legacy.ID)
	require.NoError(t, err)
	require.Nil(t, got, "legacy quarantine remains sticky")
}
