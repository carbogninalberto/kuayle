package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logoWorkspaceRepo is a one-workspace WorkspaceRepo; the embedded interface
// covers the rest.
type logoWorkspaceRepo struct {
	repository.WorkspaceRepo
	ws *domain.Workspace
}

func (r *logoWorkspaceRepo) GetBySlug(context.Context, string) (*domain.Workspace, error) {
	return r.ws, nil
}

func (r *logoWorkspaceRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Workspace, error) {
	if r.ws.ID != id {
		return nil, nil
	}
	return r.ws, nil
}

func (r *logoWorkspaceRepo) Update(_ context.Context, ws *domain.Workspace) error {
	r.ws = ws
	return nil
}

func logoTestSetup(t *testing.T) (*WorkspaceHandler, *logoWorkspaceRepo, *memStore, *domain.Workspace, uuid.UUID) {
	t.Helper()
	owner := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Test", Slug: "test", OwnerID: owner}
	repo := &logoWorkspaceRepo{ws: ws}
	users := &testUserRepo{createdUsers: map[string]*domain.User{
		"owner@example.com": {ID: owner, Email: "owner@example.com", Name: "Owner", DisplayName: "Owner"},
	}}
	svc := service.NewWorkspaceService(repo, users)
	store := &memStore{files: map[string][]byte{}}
	svc.SetLogoStore(store)
	return NewWorkspaceHandler(svc), repo, store, ws, owner
}

func uploadLogo(t *testing.T, h *WorkspaceHandler, ws *domain.Workspace, userID uuid.UUID, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	require.NoError(t, err)
	_, _ = part.Write(data)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/test/logo", &body)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set("workspace", ws)
	c.Set(string(middleware.UserIDKey), userID)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: ws.Slug}})
	require.NoError(t, h.UploadLogo(c))
	return rec
}

func serveLogo(t *testing.T, h *WorkspaceHandler, workspaceID, file string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/workspace-logos/"+workspaceID+"/"+file, nil), rec)
	c.SetPathValues(echo.PathValues{{Name: "workspaceId", Value: workspaceID}, {Name: "file", Value: file}})
	require.NoError(t, h.Logo(c))
	return rec
}

func logoURL(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		LogoURL *string `json:"logo_url"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotNil(t, body.LogoURL)
	return *body.LogoURL
}

func TestWorkspaceLogoUploadServeReplaceAndClear(t *testing.T) {
	h, repo, store, ws, owner := logoTestSetup(t)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	rec := uploadLogo(t, h, ws, owner, "logo.png", png)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	first := logoURL(t, rec)
	assert.True(t, strings.HasPrefix(first, "/api/workspace-logos/"+ws.ID.String()+"/"), first)
	file, ok := domain.WorkspaceLogoFile(ws.ID, &first)
	require.True(t, ok)
	assert.Len(t, store.files, 1)

	rec = serveLogo(t, h, ws.ID.String(), file)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "public, max-age=86400, immutable", rec.Header().Get("Cache-Control"))
	assert.Equal(t, png, rec.Body.Bytes())

	// A second upload replaces the logo and removes the old file.
	rec = uploadLogo(t, h, ws, owner, "logo.png", png)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	second := logoURL(t, rec)
	require.NotEqual(t, first, second)
	secondFile, ok := domain.WorkspaceLogoFile(ws.ID, &second)
	require.True(t, ok)
	assert.Len(t, store.files, 1, "the replaced file is deleted")
	assert.Equal(t, http.StatusNotFound, serveLogo(t, h, ws.ID.String(), file).Code)

	// Re-saving the current uploaded logo URL (the settings form does this)
	// succeeds even though it is a relative path.
	patchLogo := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/workspaces/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		c := echo.New().NewContext(req, rec)
		c.Set("workspace", ws)
		c.Set(string(middleware.UserIDKey), owner)
		c.SetPathValues(echo.PathValues{{Name: "slug", Value: ws.Slug}})
		require.NoError(t, h.Update(c))
		return rec
	}
	rec = patchLogo(`{"logo_url":"` + second + `"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, store.files, 1, "re-saving keeps the current file")
	assert.Equal(t, http.StatusOK, serveLogo(t, h, ws.ID.String(), secondFile).Code)

	// A different relative path is still rejected as an invalid URL.
	rec = patchLogo(`{"logo_url":"/api/workspace-logos/` + ws.ID.String() + `/` + uuid.NewString() + `.png"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "must be a valid URL")

	// Clearing the logo removes the current file.
	e := echo.New()
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/workspaces/test", strings.NewReader(`{"logo_url":null}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	c := e.NewContext(req, rec)
	c.Set("workspace", ws)
	c.Set(string(middleware.UserIDKey), owner)
	c.SetPathValues(echo.PathValues{{Name: "slug", Value: ws.Slug}})
	require.NoError(t, h.Update(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, store.files)
	assert.Equal(t, http.StatusNotFound, serveLogo(t, h, ws.ID.String(), secondFile).Code)
	assert.Nil(t, repo.ws.LogoURL)
}

func TestWorkspaceLogoRejectsUnsupportedAndOversizedFiles(t *testing.T) {
	h, _, store, ws, owner := logoTestSetup(t)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	rec := uploadLogo(t, h, ws, owner, "logo.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>1</script></svg>"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"UNSUPPORTED_IMAGE"`)

	rec = uploadLogo(t, h, ws, owner, "logo.txt", []byte("not an image"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"UNSUPPORTED_IMAGE"`)

	rec = uploadLogo(t, h, ws, owner, "huge.png", append(append([]byte{}, png...), bytes.Repeat([]byte{0}, domain.WorkspaceLogoMaxBytes)...))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), `"WORKSPACE_LOGO_TOO_LARGE"`)
	assert.Empty(t, store.files)
}

func TestWorkspaceLogoUnavailableWithoutStore(t *testing.T) {
	owner := uuid.New()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Test", Slug: "test", OwnerID: owner}
	svc := service.NewWorkspaceService(&logoWorkspaceRepo{ws: ws}, nil)
	h := NewWorkspaceHandler(svc)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	rec := uploadLogo(t, h, ws, owner, "logo.png", png)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), `"UNAVAILABLE"`)
}

func TestWorkspaceLogoOnlyOwnersUpload(t *testing.T) {
	h, _, store, ws, _ := logoTestSetup(t)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	rec := uploadLogo(t, h, ws, uuid.New(), "logo.png", png)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, store.files)
}

func TestWorkspaceLogoServeOnlyCurrentFile(t *testing.T) {
	h, _, store, ws, owner := logoTestSetup(t)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	rec := uploadLogo(t, h, ws, owner, "logo.png", png)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	currentURL := logoURL(t, rec)
	current, ok := domain.WorkspaceLogoFile(ws.ID, &currentURL)
	require.True(t, ok)

	unreferenced := uuid.NewString() + ".png"
	store.files[workspaceLogoKeyForTest(ws.ID, unreferenced)] = png
	assert.Equal(t, http.StatusNotFound, serveLogo(t, h, ws.ID.String(), unreferenced).Code)
	assert.Equal(t, http.StatusNotFound, serveLogo(t, h, uuid.NewString(), current).Code)
	assert.Equal(t, http.StatusNotFound, serveLogo(t, h, "not-a-uuid", current).Code)
	assert.Equal(t, http.StatusNotFound, serveLogo(t, h, ws.ID.String(), "not-a-file.png").Code)
}

func workspaceLogoKeyForTest(workspaceID uuid.UUID, file string) string {
	return "workspace-logo-" + workspaceID.String() + "-" + file
}
