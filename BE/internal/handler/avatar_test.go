package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memStore struct{ files map[string][]byte }

func (m *memStore) Put(_ context.Context, key string, r io.Reader, _ string) (int64, error) {
	b, _ := io.ReadAll(r)
	m.files[key] = b
	return int64(len(b)), nil
}
func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.files[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (m *memStore) Delete(_ context.Context, key string) error        { delete(m.files, key); return nil }
func (m *memStore) URL(_ context.Context, key string) (string, error) { return key, nil }

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func uploadAvatar(t *testing.T, h *AuthHandler, userID uuid.UUID, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	require.NoError(t, err)
	_, _ = part.Write(data)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/auth/me/avatar", &body)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set(string(middleware.UserIDKey), userID)
	require.NoError(t, h.UploadAvatar(c))
	return rec
}

func TestUploadAvatar(t *testing.T) {
	user := &domain.User{ID: uuid.New(), Email: "a@example.com", Name: "A", DisplayName: "A"}
	users := &testUserRepo{createdUsers: map[string]*domain.User{user.Email: user}}
	svc := service.NewAuthService(users, &testRefreshTokenRepo{}, "secret")
	h := NewAuthHandler(svc, false, middleware.NewLoginThrottle(5, time.Minute), nil)
	store := &memStore{files: map[string][]byte{}}
	h.SetAvatarStore(store)

	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	rec := uploadAvatar(t, h, user.ID, "x.png", []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>1</script></svg>"))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "non-raster content is rejected even with an image name")

	rec = uploadAvatar(t, h, user.ID, "x.png", append(png, bytes.Repeat([]byte{0}, maxAvatarSize)...))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Empty(t, store.files)

	rec = uploadAvatar(t, h, user.ID, "x.png", png)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, user.AvatarURL)
	assert.True(t, isOwnAvatarPath(*user.AvatarURL, user.ID))
	assert.Len(t, store.files, 1)
	first := *user.AvatarURL

	rec = uploadAvatar(t, h, user.ID, "y.png", png)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotEqual(t, first, *user.AvatarURL)
	assert.Len(t, store.files, 1, "the previous upload is removed")

	parts := strings.Split(*user.AvatarURL, "/")
	req := httptest.NewRequest(http.MethodGet, *user.AvatarURL, nil)
	rec = httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("userId", "file")
	c.SetParamValues(user.ID.String(), parts[len(parts)-1])
	require.NoError(t, h.GetAvatar(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))

	assert.False(t, isOwnAvatarPath(*user.AvatarURL, uuid.New()), "another user's avatar path cannot be claimed")
}
