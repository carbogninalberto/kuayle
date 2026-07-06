package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/assettoken"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryStorage struct {
	files map[string]string
}

func (s *memoryStorage) Put(_ context.Context, key string, r io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	if s.files == nil {
		s.files = make(map[string]string)
	}
	s.files[key] = string(data)
	return int64(len(data)), nil
}

func (s *memoryStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.files[key])), nil
}

func (s *memoryStorage) Delete(_ context.Context, key string) error { return nil }
func (s *memoryStorage) URL(_ context.Context, key string) (string, error) {
	return "/uploads/" + key, nil
}

type memoryAssetRepo struct {
	assets map[uuid.UUID]*domain.Asset
}

func (r *memoryAssetRepo) Create(_ context.Context, asset *domain.Asset) error {
	if r.assets == nil {
		r.assets = make(map[uuid.UUID]*domain.Asset)
	}
	r.assets[asset.ID] = asset
	return nil
}

func (r *memoryAssetRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Asset, error) {
	asset := r.assets[id]
	return asset, nil
}

func TestUploadCreatesProtectedAssetURL(t *testing.T) {
	e := echo.New()
	store := &memoryStorage{}
	assetRepo := &memoryAssetRepo{}
	h := NewUploadHandler(store, assetRepo, nil, "secret-32-characters-minimum-value")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="screenshot.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write([]byte("image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/acme/upload", body)
	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("workspace", &domain.Workspace{ID: uuid.New(), Slug: "acme"})
	c.Set("user_id", uuid.New())

	err = h.Upload(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `/api/workspaces/acme/assets/`)
	assert.Len(t, assetRepo.assets, 1)
	for _, asset := range assetRepo.assets {
		assert.Equal(t, "screenshot.png", asset.Filename)
		assert.Equal(t, "image/png", asset.ContentType)
		assert.Equal(t, "image bytes", store.files[asset.StorageKey])
	}
}

func TestGetAssetStreamsWorkspaceAsset(t *testing.T) {
	e := echo.New()
	assetID := uuid.New()
	workspaceID := uuid.New()
	store := &memoryStorage{files: map[string]string{"asset.png": "image bytes"}}
	assetRepo := &memoryAssetRepo{assets: map[uuid.UUID]*domain.Asset{
		assetID: {
			ID:          assetID,
			WorkspaceID: workspaceID,
			StorageKey:  "asset.png",
			ContentType: "image/png",
		},
	}}
	h := NewUploadHandler(store, assetRepo, nil, "secret-32-characters-minimum-value")

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/acme/assets/"+assetID.String(), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("workspace", &domain.Workspace{ID: workspaceID, Slug: "acme"})
	c.SetParamNames("assetId")
	c.SetParamValues(assetID.String())

	err := h.GetAsset(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "image bytes", rec.Body.String())
}

func TestPublicAssetRequiresValidToken(t *testing.T) {
	e := echo.New()
	assetID := uuid.New()
	workspaceID := uuid.New()
	issueID := uuid.New()
	store := &memoryStorage{files: map[string]string{"asset.png": "image bytes"}}
	assetRepo := &memoryAssetRepo{assets: map[uuid.UUID]*domain.Asset{
		assetID: {
			ID:          assetID,
			WorkspaceID: workspaceID,
			StorageKey:  "asset.png",
			ContentType: "image/png",
		},
	}}
	h := NewUploadHandler(store, assetRepo, nil, "secret-32-characters-minimum-value")
	token, _, err := assettoken.Generate("secret-32-characters-minimum-value:prompt-assets", assetID, workspaceID, issueID, time.Hour)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/public/assets/"+token, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("token")
	c.SetParamValues(token)

	err = h.PublicAsset(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image bytes", rec.Body.String())
}

func TestExtractProtectedAssetSourcesIgnoresExternalImages(t *testing.T) {
	assetID := uuid.New()
	html := `<p><img src="/api/workspaces/acme/assets/` + assetID.String() + `"><img src="https://example.com/image.png"></p>`

	sources := extractProtectedAssetSources(html)

	require.Len(t, sources, 1)
	assert.Equal(t, assetID, sources[0].assetID)
}
