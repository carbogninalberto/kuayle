package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type memProjectRepo struct{ project *domain.Project }

func (r *memProjectRepo) Create(_ context.Context, p *domain.Project) error {
	r.project = p
	return nil
}
func (r *memProjectRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Project, error) {
	if r.project != nil && r.project.ID == id {
		copied := *r.project
		return &copied, nil
	}
	return nil, nil
}
func (r *memProjectRepo) ListByWorkspace(context.Context, uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (r *memProjectRepo) ListByTeam(context.Context, uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (r *memProjectRepo) Update(_ context.Context, p *domain.Project) error {
	r.project = p
	return nil
}
func (r *memProjectRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (r *memProjectRepo) IssueStats(context.Context, uuid.UUID) (int, int, int, error) {
	return 0, 0, 0, nil
}

func patchProject(t *testing.T, h *ProjectHandler, ws *domain.Workspace, id uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "id", Value: id.String()}})
	c.Set("workspace", ws)
	require.NoError(t, h.Update(c))
	return rec
}

func TestProjectUpdateValidatesColorAndIcon(t *testing.T) {
	ws := &domain.Workspace{ID: uuid.New()}
	repo := &memProjectRepo{project: &domain.Project{ID: uuid.New(), WorkspaceID: ws.ID, Name: "Launch"}}
	h := NewProjectHandler(service.NewProjectService(repo))
	id := repo.project.ID

	for name, body := range map[string]string{
		"colour name":   `{"color":"blue"}`,
		"icon too long": `{"icon":"` + strings.Repeat("a", 51) + `"}`,
	} {
		rec := patchProject(t, h, ws, id, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, name)
		require.Nil(t, repo.project.Color, name)
		require.Nil(t, repo.project.Icon, name)
	}

	rec := patchProject(t, h, ws, id, `{"color":"#3b82f6","icon":"Rocket"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "#3b82f6", *repo.project.Color)
	require.Equal(t, "Rocket", *repo.project.Icon)

	rec = patchProject(t, h, ws, id, `{"color":"","icon":""}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, repo.project.Color)
	require.Nil(t, repo.project.Icon)
}
