package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iconTeamRepo is a one-team TeamRepo; the embedded interface covers the rest.
type iconTeamRepo struct {
	repository.TeamRepo
	team *domain.Team
}

func (r *iconTeamRepo) GetByID(context.Context, uuid.UUID) (*domain.Team, error) { return r.team, nil }
func (r *iconTeamRepo) Update(context.Context, *domain.Team) error               { return nil }
func (r *iconTeamRepo) SetIcon(_ context.Context, _ uuid.UUID, icon string) (*domain.Team, error) {
	r.team.ReplacedIcon, r.team.Icon = r.team.Icon, &icon
	return r.team, nil
}

func TestTeamIconUploadAndServe(t *testing.T) {
	ws := &domain.Workspace{ID: uuid.New()}
	team := &domain.Team{ID: uuid.New(), WorkspaceID: ws.ID}
	svc := service.NewTeamService(&iconTeamRepo{team: team}, nil)
	store := &memStore{files: map[string][]byte{}}
	svc.SetIconStore(store)
	h := NewTeamHandler(svc)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)

	upload := func(data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, err := w.CreateFormFile("file", "favicon.png")
		require.NoError(t, err)
		_, _ = part.Write(data)
		require.NoError(t, w.Close())
		req := httptest.NewRequest(http.MethodPost, "/icon", &body)
		req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.Set("workspace", ws)
		c.SetPathValues(echo.PathValues{{Name: "teamId", Value: team.ID.String()}})
		require.NoError(t, h.UploadIcon(c))
		return rec
	}

	rec := upload([]byte("<svg xmlns='http://www.w3.org/2000/svg'><script>1</script></svg>"))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "SVG is rejected")
	rec = upload(append(append([]byte{}, png...), bytes.Repeat([]byte{0}, domain.TeamIconMaxBytes)...))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), `"TEAM_ICON_TOO_LARGE"`)
	assert.Empty(t, store.files)

	rec = upload(png)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, team.Icon)
	file, ok := domain.TeamIconImageFile(team.Icon)
	require.True(t, ok)
	assert.Len(t, store.files, 1)

	serve := func(name string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/icon/"+name, nil), rec)
		c.Set("workspace", ws)
		c.SetPathValues(echo.PathValues{{Name: "teamId", Value: team.ID.String()}, {Name: "file", Value: name}})
		require.NoError(t, h.Icon(c))
		return rec
	}
	rec = serve(file)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, http.StatusNotFound, serve(strings.Replace(file, file[:4], "ffff", 1)).Code)
}

// An animated PNG is detected as image/vnd.mozilla.apng; it is accepted and
// stored and served as a PNG, which browsers animate.
func TestTeamIconUploadAcceptsAnimatedPNG(t *testing.T) {
	ws := &domain.Workspace{ID: uuid.New()}
	team := &domain.Team{ID: uuid.New(), WorkspaceID: ws.ID}
	svc := service.NewTeamService(&iconTeamRepo{team: team}, nil)
	svc.SetIconStore(&memStore{files: map[string][]byte{}})
	h := NewTeamHandler(svc)
	png, _ := base64.StdEncoding.DecodeString(tinyPNG)
	// Insert an acTL (animation control) chunk right after IHDR.
	actl := make([]byte, 0, 20)
	actl = binary.BigEndian.AppendUint32(actl, 8)
	actl = append(actl, "acTL"...)
	actl = binary.BigEndian.AppendUint32(actl, 1)
	actl = binary.BigEndian.AppendUint32(actl, 0)
	actl = binary.BigEndian.AppendUint32(actl, crc32.ChecksumIEEE(actl[4:]))
	apng := append(append(append([]byte{}, png[:33]...), actl...), png[33:]...)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "animated.png")
	require.NoError(t, err)
	_, _ = part.Write(apng)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/icon", &body)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set("workspace", ws)
	c.SetPathValues(echo.PathValues{{Name: "teamId", Value: team.ID.String()}})
	require.NoError(t, h.UploadIcon(c))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	file, ok := domain.TeamIconImageFile(team.Icon)
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(file, ".png"))
	assert.Equal(t, "image/png", domain.TeamIconContentType(file))
}
