package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestTransferHTTPErrorReturnsMissingUsersAsStructuredDetails(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/workspaces/import", nil), recorder)

	err := transferHTTPError(ctx, &service.WorkspaceTransferError{
		Code: "WORKSPACE_IMPORT_MISSING_USERS", Message: "Missing users",
		MissingUsers: []string{"One@Example.test", "two@example.test"},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{
		"error": {
			"code": "WORKSPACE_IMPORT_MISSING_USERS",
			"message": "Missing users",
			"details": [
				{"field":"missing_user","message":"one@example.test"},
				{"field":"missing_user","message":"two@example.test"}
			]
		}
	}`, recorder.Body.String())
}

func TestTransferHTTPErrorReturnsSlugConflict(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/api/workspaces/import", nil), recorder)

	err := transferHTTPError(ctx, &service.WorkspaceTransferError{Code: "WORKSPACE_SLUG_TAKEN", Message: "Workspace slug is already taken"})

	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
}

func TestWorkspaceTransferExportReturnsZIPWithOrphanedSharedLink(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	userID := uuid.New()
	workspaceID := uuid.New()
	teamID := uuid.New()
	slug := "transfer-handler-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	_, err = db.Exec(`INSERT INTO users(id,email,name,display_name,password_hash) VALUES($1,$2,'Transfer User','Transfer User','unusable')`, userID, "transfer-handler-"+userID.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Transfer Handler',$2,$3)`, workspaceID, slug, userID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspaceID) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspaceID, userID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, teamID, workspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO shared_links(id,token,workspace_id,created_by,scope,scope_id,filters,include_description,is_active) VALUES($1,$2,$3,$4,'team',$5,'{}',true,true)`, uuid.New(), uuid.NewString(), workspaceID, userID, teamID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO shared_links(id,token,workspace_id,created_by,scope,scope_id,filters,include_description,is_active) VALUES($1,$2,$3,$4,'team',$5,'{}',true,true)`, uuid.New(), uuid.NewString(), workspaceID, userID, uuid.New())
	require.NoError(t, err)

	backend, err := storage.NewLocalBackend(t.TempDir(), "")
	require.NoError(t, err)
	handler := NewWorkspaceTransferHandler(service.NewWorkspaceTransferService(repository.NewWorkspaceTransferRepository(db), backend))
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/workspaces/"+slug+"/export", nil), recorder)
	ctx.Set("workspace", &domain.Workspace{ID: workspaceID, Name: "Transfer Handler", Slug: slug, OwnerID: userID})
	ctx.Set(string(middleware.UserIDKey), userID)

	require.NoError(t, handler.Export(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/zip", recorder.Header().Get(echo.HeaderContentType))
	require.True(t, bytes.HasPrefix(recorder.Body.Bytes(), []byte("PK")), "export must return a ZIP payload")
}
